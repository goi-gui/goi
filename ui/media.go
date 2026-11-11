package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/media"
	"github.com/goi-gui/goi/internal/platform"
	"github.com/goi-gui/goi/internal/tree"
)

// Media playback is split in two so the UI never waits for a decoder:
//
//   - a pump goroutine owns the ffmpeg process and fills a bounded queue;
//   - the frame loop takes from that queue when the clock says a frame is due.
//
// The queue is what "buffering" means here, and it is deliberately small: the
// player stays `buffer` seconds ahead, capped by a byte budget, so a long film
// never lands in RAM while a slow disk or a slow network still has room to
// breathe. When the queue is full the pump blocks, which is how backpressure
// works without a scheduler of our own.

const (
	// audioRate and audioChannels are the format every decoder converts to, so
	// one device serves an mp3 and a 48 kHz film alike.
	audioRate     = 48000
	audioChannels = 2
	// DefaultBuffer is how far ahead a player decodes when the design does not
	// say. Long enough to ride out a hiccup, short enough to seek quickly.
	DefaultBuffer = 1.5
	// maxBufferSeconds caps the property: a minute of video in RAM is a bug,
	// not a feature.
	maxBufferSeconds = 10.0
	// Video and audio byte budgets. A buffer is measured in seconds, but memory
	// is what runs out, so the seconds are clamped to these.
	maxVideoBufferBytes = 64 << 20
	// minVideoFrames keeps a buffer useful at a low frame rate.
	minVideoFrames = 2
)

// mediaPlayer is one running decoder for one node.
type mediaPlayer struct {
	node tree.Node
	m    *tree.Media

	src  string
	info media.Info

	// frame is the current video frame, which is also what the node paints.
	frame *img.Data
	// canvas is the RGBA buffer the node paints, at the decoded size.
	// frameAt is when the next frame is due, from the wall clock.
	nextFrame time.Time

	// audio is the playback device; nil when the file has no sound or the
	// machine has none.
	audio *platform.AudioStream

	// Buffered frames. The pump decodes into a buffer from free and sends it to
	// frames; the UI takes from frames and hands the buffer back. Sending on a
	// full channel blocks, which is the whole flow control.
	frames chan []byte
	free   chan []byte

	mu      sync.Mutex
	ended   bool
	failure error
	stop    chan struct{}
	wg      sync.WaitGroup

	// opened is the offset the decoder started at, which a seek changes.
	opened time.Duration
	// buffer is the queue depth in seconds, from the design.
	buffer float64
	// volume/muted are applied on the next chunk of audio.
	volume float64
	muted  bool
}

// loadMedia starts a player for every Audio and Video in a freshly built tree,
// the way loadImages does. Only a node that should play gets a decoder; the
// rest wait for Play, which keeps a page of paused players free.
func (a *App) loadMedia(tr *tree.Tree) {
	a.hasMedia = false
	a.media = a.media[:0]
	if tr == nil {
		return
	}
	for _, n := range tr.All() {
		if _, ok := tree.MediaNode(n); !ok {
			continue
		}
		a.hasMedia = true
		m, _ := tree.MediaNode(n)
		m.RenderedSrc = m.Src.Get()
		if m.Src.Get() != "" {
			// Probe now, so the layout knows the aspect and the duration before
			// the first frame; the decoder itself starts only when playing.
			a.probeMedia(n, m)
		}
	}
}

// resolveMedia gives a src a form ffmpeg can open. A file inside the design's
// container has no path of its own, so it is materialised once into a temp file
// and remembered for the run; every other src is already a path or a URL.
func (a *App) resolveMedia(src string) (string, error) {
	if src == "" {
		return "", fmt.Errorf("ui: empty src")
	}
	if isRemoteSrc(src) || filepath.IsAbs(src) {
		return src, nil
	}
	if a.mediaFiles == nil {
		a.mediaFiles = map[string]string{}
	}
	if p, ok := a.mediaFiles[src]; ok {
		return p, nil
	}
	// A `goi://` path reads the design's container; a bare relative path is
	// tried there first, then left to the working directory, which is what a
	// `src: "clip.mp4"` next to the design means.
	b, err := a.readSrc(assetScheme + strings.TrimPrefix(src, "./"))
	if err != nil {
		b, err = a.readSrc(src)
	}
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "goi-media-*"+filepath.Ext(src))
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	a.mediaFiles[src] = f.Name()
	return f.Name(), nil
}

// probeMedia reads the stream properties once per src, so a video knows its
// size and a control bar knows its length before anything plays.
func (a *App) probeMedia(n tree.Node, m *tree.Media) {
	src := m.Src.Get()
	path, err := a.resolveMedia(src)
	if err != nil {
		m.LoadErr = err
		m.Buffering = false
		return
	}
	if info, ok := a.mediaInfo[src]; ok {
		m.Duration = info.Duration
		m.Buffering = false
		return
	}
	info, err := media.Probe(path)
	if err != nil {
		m.LoadErr = err
		m.Buffering = false
		return
	}
	if a.mediaInfo == nil {
		a.mediaInfo = map[string]media.Info{}
	}
	a.mediaInfo[src] = info
	m.Duration = info.Duration
	m.LoadErr = nil
	m.Buffering = false
	if v, ok := n.(*tree.Video); ok && info.HasVideo {
		v.FrameW, v.FrameH = info.Width, info.Height
		v.Base().MarkDirty(tree.DirtyLayout)
	}
}

// playerFor returns the running player for a node, starting one when the node
// asked to play.
func (a *App) playerFor(n tree.Node) *mediaPlayer {
	for _, p := range a.media {
		if p.node == n {
			return p
		}
	}
	return nil
}

// startPlayer opens the decoders for a node and starts pumping.
func (a *App) startPlayer(n tree.Node) (*mediaPlayer, error) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return nil, fmt.Errorf("ui: %s is not a media element", n.Kind())
	}
	src := m.Src.Get()
	if src == "" {
		return nil, fmt.Errorf("ui: %s has no src", n.Kind())
	}
	if p := a.playerFor(n); p != nil {
		return p, nil
	}
	path, err := a.resolveMedia(src)
	if err != nil {
		return nil, err
	}
	p := &mediaPlayer{node: n, m: m, src: path, volume: 1, muted: m.Muted.Get(),
		stop: make(chan struct{})}
	p.volume = clampUnit(m.Volume.Get())
	p.muted = m.Muted.Get()
	info := a.mediaInfo[src]
	if info.Width == 0 && info.Height == 0 && !info.HasAudio {
		probed, err := media.Probe(path)
		if err != nil {
			return nil, err
		}
		info = probed
	}
	p.info = info
	p.buffer = bufferSeconds(m.Buffer.Get())
	p.opened = time.Duration(m.StartAt.Get() * float64(time.Second))
	m.Duration = info.Duration

	// A video needs a canvas to paint into. It is decoded near the size it is
	// drawn at, so a 4K file in a 480dp box does not decode 8 megapixels a
	// frame.
	if v, ok := n.(*tree.Video); ok && info.HasVideo {
		w, h := decodeSize(info, v.Base().Bounds())
		v.FrameW, v.FrameH = w, h
		if v.Data == nil || v.Data.W != w || v.Data.H != h {
			v.Data = &img.Data{W: w, H: h, Pix: make([]byte, w*h*4)}
		}
		p.frame = v.Data
		p.openFrames(w * h * 4)
	}

	if info.HasAudio && !m.Muted.Get() {
		// A machine with no sound card returns nil, and the video still plays.
		p.audio = platform.OpenAudio(audioRate, audioChannels)
	}

	a.media = append(a.media, p)
	p.start()
	return p, nil
}

// decodeSize picks the frame size a video decodes at: the file's own size,
// shrunk to what the box can actually show, and capped so a huge file cannot
// spend the whole frame budget on one picture.
func decodeSize(info media.Info, box tree.Rect) (int, int) {
	w, h := info.Width, info.Height
	if w <= 0 || h <= 0 {
		return 16, 16
	}
	// Twice the drawn size is enough for a sharp picture at any scale factor.
	maxW, maxH := 1280.0, 1280.0
	if box.W > 0 && box.H > 0 {
		if v := box.W * 2; v < maxW {
			maxW = max(max(v, 64), 2)
		}
		if v := box.H * 2; v < maxH {
			maxH = max(max(v, 64), 2)
		}
	}
	scale := min(1, min(maxW/float64(w), maxH/float64(h)))
	sw, sh := int(float64(w)*scale), int(float64(h)*scale)
	// Even numbers keep every codec's chroma happy, and 2 is the floor.
	sw &^= 1
	sh &^= 1
	if sw < 2 {
		sw = 2
	}
	if sh < 2 {
		sh = 2
	}
	return sw, sh
}

// bufferSeconds clamps the design's buffer to something a machine can hold.
func bufferSeconds(v float64) float64 {
	if v <= 0 {
		return DefaultBuffer
	}
	if v > maxBufferSeconds {
		return maxBufferSeconds
	}
	return v
}

// openFrames sizes the frame queue: `buffer` seconds of video at the file's
// frame rate, then clamped so the bytes fit the budget.
func (p *mediaPlayer) openFrames(frameBytes int) {
	fps := p.info.FPS
	if fps <= 0 {
		fps = 30
	}
	n := int(p.buffer*fps + 0.5)
	if n < minVideoFrames {
		n = minVideoFrames
	}
	if limit := maxVideoBufferBytes / max(frameBytes, 1); n > limit {
		n = limit
	}
	if n < 1 {
		n = 1
	}
	p.frames = make(chan []byte, n)
	p.free = make(chan []byte, n)
	for i := 0; i < n; i++ {
		p.free <- make([]byte, frameBytes)
	}
}

// buffered is how much video is waiting, in frames.
func (p *mediaPlayer) buffered() int {
	if p.frames == nil {
		return 0
	}
	return len(p.frames)
}

// start launches the two pumps: video frames into the queue, audio into the
// device.
func (p *mediaPlayer) start() {
	p.wg.Add(2)
	go p.pumpVideo()
	go p.pumpAudio()
}

// pumpVideo decodes frames as fast as ffmpeg produces them and keeps only the
// newest, which is frame dropping done properly: the UI takes what it wants and
// the rest is overwritten, never queued.
func (p *mediaPlayer) pumpVideo() {
	defer p.wg.Done()
	if !p.info.HasVideo || p.frames == nil {
		return
	}
	dec, err := media.OpenVideo(p.src, p.frame.W, p.frame.H, p.opened)
	if err != nil {
		p.fail(err)
		return
	}
	defer dec.Close()
	for {
		// Take a free buffer; a full queue blocks here, which is the
		// backpressure that keeps memory flat.
		var buf []byte
		select {
		case <-p.stop:
			return
		case buf = <-p.free:
		}
		if _, err := dec.Read(buf); err != nil {
			select {
			case p.free <- buf:
			default:
			}
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				p.mu.Lock()
				p.ended = true
				p.mu.Unlock()
			} else {
				p.fail(err)
			}
			return
		}
		select {
		case <-p.stop:
			return
		case p.frames <- buf:
		}
	}
}

// pumpAudio streams PCM into the device, staying one buffer ahead. Sleeping
// while the device's queue is full is what keeps a two hour podcast from
// decoding into memory all at once.
func (p *mediaPlayer) pumpAudio() {
	defer p.wg.Done()
	if p.audio == nil {
		return
	}
	dec, err := media.OpenPCM(p.src, audioRate, audioChannels, p.opened)
	if err != nil {
		return // a silent file is not a failure
	}
	defer dec.Close()
	chunk := make([]byte, 16*1024)
	leadBytes := p.audioLead()
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		for p.audio.Queued() > leadBytes {
			select {
			case <-p.stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
		n, err := dec.Read(chunk)
		if n > 0 {
			p.audio.Queue(volumeScale(chunk[:n], p.volume, p.muted))
		}
		if err != nil {
			return
		}
	}
}

// audioLead is how many bytes of sound the device may hold: the design's buffer
// in seconds, which the property already clamps to ten seconds of sound.
func (p *mediaPlayer) audioLead() int {
	bytes := int(p.buffer * float64(audioRate*audioChannels*2))
	if min := audioRate * audioChannels * 2 / 10; bytes < min {
		bytes = min // at least 100ms, or the device runs dry between chunks
	}
	return bytes
}

// volumeScale applies the design's volume to a chunk of 16-bit stereo PCM. It
// runs on the pump goroutine, so a volume change never costs a frame.
func volumeScale(pcm []byte, volume float64, muted bool) []byte {
	if muted || volume <= 0.001 {
		return make([]byte, len(pcm))
	}
	if volume >= 0.999 {
		return pcm
	}
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8)
		v := int32(float64(s) * volume)
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		pcm[i] = byte(v)
		pcm[i+1] = byte(v >> 8)
	}
	return pcm
}

func (p *mediaPlayer) fail(err error) {
	p.mu.Lock()
	if p.failure == nil {
		p.failure = err
	}
	p.mu.Unlock()
}

// close stops both pumps and the device.
func (p *mediaPlayer) close() {
	select {
	case <-p.stop:
	default:
		close(p.stop)
	}
	// A pump blocked on a full queue is released by the stop case, but a pump
	// blocked handing a frame over is not holding the queue; draining here just
	// makes the wait prompt.
	p.wg.Wait()
	if p.audio != nil {
		p.audio.Close()
		p.audio = nil
	}
}

// MediaRequest runs one playback method a design called by id.
func (a *App) mediaRequest(n tree.Node, method string, args []any) bool {
	m, ok := tree.MediaNode(n)
	if !ok {
		return false
	}
	switch method {
	case "play":
		a.playMedia(n)
	case "pause":
		a.pauseMedia(n)
	case "stop":
		a.stopMedia(n)
	case "toggle":
		if m.Playing && !m.Paused {
			a.pauseMedia(n)
		} else {
			a.playMedia(n)
		}
	case "fullscreen":
		a.setFullscreen(n, true)
	case "windowed":
		a.setFullscreen(n, false)
	case "toggleFullscreen":
		if v, ok := n.(*tree.Video); ok {
			a.setFullscreen(n, !v.Fullscreen.Get())
		}
	case "seek":
		if len(args) != 1 {
			return false
		}
		sec, ok := toFloat(args[0])
		if !ok {
			return false
		}
		a.seekMedia(n, time.Duration(sec*float64(time.Second)))
	case "volume":
		if len(args) != 1 {
			return false
		}
		sec, ok := toFloat(args[0])
		if !ok {
			return false
		}
		a.setMediaVolume(n, sec)
	default:
		return false
	}
	return true
}

// toFloat accepts the numeric shapes a bridge argument can arrive as.
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

// playMedia starts or resumes a player.
func (a *App) playMedia(n tree.Node) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	if m.Ended {
		a.seekMedia(n, 0)
	}
	p, err := a.startPlayer(n)
	if err != nil {
		m.LoadErr = err
		m.Buffering = false
		m.Base().MarkDirty(tree.DirtyLayout)
		return
	}
	m.Playing, m.Paused, m.Ended = true, false, false
	if p.audio != nil {
		p.audio.Resume()
	}
	m.Playback.Emit(true)
	m.Base().MarkDirty(tree.DirtyPaint)
	a.markDirty(tree.DirtyPaint)
	a.needsFrame = true
}

// pauseMedia stops the clock and the sound, keeping the position.
func (a *App) pauseMedia(n tree.Node) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	if p := a.playerFor(n); p != nil && p.audio != nil {
		p.audio.Pause()
	}
	if !m.Playing || m.Paused {
		return
	}
	m.Paused = true
	m.Playback.Emit(false)
	m.Base().MarkDirty(tree.DirtyPaint)
	a.markDirty(tree.DirtyPaint)
}

// stopMedia rewinds and holds the first frame.
func (a *App) stopMedia(n tree.Node) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	if p := a.playerFor(n); p != nil {
		p.close()
		a.dropPlayer(n)
	}
	m.Playing, m.Paused, m.Ended = false, false, false
	m.Position = 0
	if v, ok := n.(*tree.Video); ok {
		if v.Data != nil && len(v.Data.Pix) > 0 {
			for i := range v.Data.Pix {
				v.Data.Pix[i] = 0
			}
			v.Data.Gen++
		}
	}
	m.Playback.Emit(false)
	m.Base().MarkDirty(tree.DirtyPaint)
	a.markDirty(tree.DirtyPaint)
}

// seekMedia restarts the decoders at an offset, which is the only seek ffmpeg
// offers without a custom protocol handler.
func (a *App) seekMedia(n tree.Node, at time.Duration) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	if at < 0 {
		at = 0
	}
	if m.Duration > 0 && at > m.Duration {
		at = m.Duration
	}
	playing := m.Playing && !m.Paused
	if p := a.playerFor(n); p != nil {
		p.close()
		a.dropPlayer(n)
	}
	m.Position = at
	m.Ended = false
	m.Seeked.Emit(at)
	if playing {
		a.playMedia(n)
	} else {
		m.Base().MarkDirty(tree.DirtyPaint)
		a.markDirty(tree.DirtyPaint)
	}
}

// setFullscreen puts a video over the whole window. The window itself follows
// in syncVideoFullscreen, which also covers a design that writes
// `fullscreen: true` and never calls the method.
func (a *App) setFullscreen(n tree.Node, on bool) {
	v, ok := n.(*tree.Video)
	if !ok {
		return
	}
	if v.Fullscreen.Get() == on {
		return
	}
	v.Fullscreen.Set(on)
	v.Base().MarkDirty(tree.DirtyLayout)
	a.markDirty(tree.DirtyLayout)
}

// syncVideoFullscreen makes the window follow the videos: a fullscreen video
// needs the platform window fullscreen too, or it fills a smaller box with a
// title bar over it; leaving the last one restores whatever the design asked
// for.
func (a *App) syncVideoFullscreen() {
	if a.win == nil || a.win.tree == nil {
		return
	}
	on := tree.FullscreenVideo(a.win.tree) != nil
	if on {
		if a.fullscreenRestore != nil || a.win.tree.Root.Fullscreen.Get() {
			return
		}
		prev := a.win.tree.Root.Fullscreen.Get()
		a.fullscreenRestore = &prev
		a.win.tree.Root.Fullscreen.Set(true)
		return
	}
	if a.fullscreenRestore == nil {
		return
	}
	a.win.tree.Root.Fullscreen.Set(*a.fullscreenRestore)
	a.fullscreenRestore = nil
}

// setMediaVolume applies a new volume, clamped to 0..1.
func (a *App) setMediaVolume(n tree.Node, v float64) {
	if m, ok := tree.MediaNode(n); ok {
		m.Volume.Set(clampUnit(v))
		if p := a.playerFor(n); p != nil {
			p.mu.Lock()
			p.volume = clampUnit(v)
			p.mu.Unlock()
		}
	}
}

// dropPlayer forgets a player without closing it again.
func (a *App) dropPlayer(n tree.Node) {
	for i, p := range a.media {
		if p.node == n {
			a.media = append(a.media[:i], a.media[i+1:]...)
			return
		}
	}
}

// clampUnit holds a number in 0..1.
func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// syncMedia follows the design: a src change restarts the player, a muted or
// volume change is applied to the running one, and an autoplay node starts on
// the frame it appears.
func (a *App) syncMedia() bool {
	if a.win == nil || a.win.tree == nil || (!a.hasMedia && !a.needsFrame) {
		return false
	}
	changed := false
	a.hasMedia = false
	a.win.tree.Walk(func(n tree.Node) {
		m, ok := tree.MediaNode(n)
		if !ok {
			return
		}
		a.hasMedia = true
		src := m.Src.Get()
		if src != m.RenderedSrc {
			if p := a.playerFor(n); p != nil {
				p.close()
				a.dropPlayer(n)
			}
			m.RenderedSrc = src
			m.LoadErr = nil
			m.Duration = 0
			m.Position = 0
			m.Playing, m.Paused, m.Ended = false, false, false
			if src != "" {
				a.probeMedia(n, m)
			}
			changed = true
		}
		if p := a.playerFor(n); p != nil {
			p.mu.Lock()
			p.volume, p.muted = clampUnit(m.Volume.Get()), m.Muted.Get()
			p.mu.Unlock()
		}
		if m.Autoplay.Get() && !m.Playing && !m.Paused && src != "" && m.LoadErr == nil {
			a.playMedia(n)
			changed = true
		}
	})
	return changed
}

// tickMedia advances every playing node by dt seconds and reports whether a
// frame is due, plus how long the loop may sleep.
func (a *App) tickMedia(dt float64) (bool, time.Duration) {
	if len(a.media) == 0 {
		return false, 0
	}
	var changed bool
	var wait time.Duration
	for i := 0; i < len(a.media); i++ {
		p := a.media[i]
		m := p.m
		if !m.Playing || m.Paused {
			continue
		}
		m.Position += time.Duration(dt * float64(time.Second))
		if m.Duration > 0 && m.Position >= m.Duration {
			if m.Loop.Get() {
				p.close()
				a.dropPlayer(p.node)
				m.Position = 0
				a.playMedia(p.node)
				changed = true
				continue
			}
			p.close()
			a.dropPlayer(p.node)
			m.Playing, m.Paused, m.Ended = false, false, true
			m.Position = m.Duration
			m.End.Emit(m.Duration)
			m.Base().MarkDirty(tree.DirtyPaint)
			changed = true
			i--
			continue
		}

		// A video paints the next decoded frame when its time comes; whatever
		// else is queued stays buffered for the frames after it.
		if v, ok := p.node.(*tree.Video); ok && p.info.HasVideo {
			due := p.nextFrame.IsZero() || !time.Now().Before(p.nextFrame)
			if due {
				if p.takeFrame() {
					v.Data.Gen++
					v.Base().MarkDirty(tree.DirtyPaint)
					changed = true
				}
				fps := p.info.FPS
				if fps <= 0 {
					fps = 30
				}
				p.nextFrame = time.Now().Add(time.Duration(float64(time.Second) / fps))
			}
			// The buffer is what the bar draws behind the position, so the
			// reader can see how far ahead the file really is.
			fps := p.info.FPS
			if fps <= 0 {
				fps = 30
			}
			if got := time.Duration(float64(p.buffered()) / fps * float64(time.Second)); got != m.Buffered {
				m.Buffered = got
				m.Base().MarkDirty(tree.DirtyPaint)
			}
			if d := time.Until(p.nextFrame); d > 0 && (wait == 0 || d < wait) {
				wait = d
			}
			if m.Buffering {
				m.Buffering = false
				changed = true
			}
		}

		// An audio player only watches for the end; the device pulls the sound.
		if p.audio != nil {
			queued := time.Duration(float64(p.audio.Queued()) / float64(audioRate*audioChannels*2) * float64(time.Second))
			if queued != m.Buffered {
				m.Buffered = queued
				m.Base().MarkDirty(tree.DirtyPaint)
			}
		}
		if p.audio != nil && p.audio.Queued() == 0 && p.reachedEnd() {
			// The queue emptying after the file ended is the real end of
			// playback, which is what a listener hears.
			if m.Loop.Get() {
				p.close()
				a.dropPlayer(p.node)
				m.Position = 0
				a.playMedia(p.node)
				changed = true
			} else {
				p.close()
				a.dropPlayer(p.node)
				m.Playing, m.Paused, m.Ended = false, false, true
				m.End.Emit(m.Position)
				changed = true
			}
			i--
			continue
		}
		if err := p.err(); err != nil {
			m.LoadErr = err
			m.Playing, m.Paused = false, false
			p.close()
			a.dropPlayer(p.node)
			m.Base().MarkDirty(tree.DirtyLayout)
			changed = true
			i--
			continue
		}
	}
	// A paused player keeps a bar ticking nothing, so the loop may sleep.
	return changed, wait
}

// takeFrame moves the newest decoded frame into the node's canvas.
func (p *mediaPlayer) takeFrame() bool {
	if p.frames == nil || p.frame == nil {
		return false
	}
	select {
	case buf := <-p.frames:
		copy(p.frame.Pix, buf)
		// Hand the buffer straight back, so the pump can refill it.
		select {
		case p.free <- buf:
		default:
		}
		return true
	default:
		return false
	}
}

// ended reports whether the decoder reached the end of the file.
func (p *mediaPlayer) reachedEnd() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ended
}

// err returns the decoder failure, if any.
func (p *mediaPlayer) err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failure
}

// closeMedia stops every player, for a window closing or a hot reload.
func (a *App) closeMedia() {
	for _, p := range a.media {
		p.close()
	}
	a.media = nil
	a.hasMedia = false
	// The temp files exist only to give ffmpeg a path; they go with the app.
	for _, path := range a.mediaFiles {
		os.Remove(path)
	}
	a.mediaFiles = nil
}
