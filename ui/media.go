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
	"github.com/goi-gui/goi/internal/subtitle"
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
	// maxLiveBufferBytes is the same budget for a live feed. A feed arrives in
	// segments with gaps between them, so the seconds the design asks for only
	// help if the bytes are allowed; a live buffer is also short-lived, because
	// it never holds more than the seconds it names.
	maxLiveBufferBytes = 96 << 20
	// minVideoFrames keeps a buffer useful at a low frame rate.
	minVideoFrames = 2
	// reserveSeconds is the cushion a live player keeps: how much it gathers
	// before the first frame, and the depth it steers back to when the feed hands
	// over segments with gaps between them.
	reserveSeconds = 3.0
	// liveDefaultBuffer is the default for a live feed. An HLS or DASH source
	// hands over a whole segment at a time and then waits, so 1.5 seconds cannot
	// cover the gap between two segments: the picture drains its queue and
	// freezes until the next one lands. Three seconds spans a segment plus the
	// playlist refresh, which is what makes the picture continuous.
	liveDefaultBuffer = 4.0
	// liveRetry is how long a dropped live feed is left alone before the
	// decoder is reopened; short enough to be a blink, long enough not to spin
	// on a server that is refusing the connection.
	liveRetry = time.Second
	// controlsHide is how long the control bar stays after the pointer stops
	// moving over a player, or leaves it.
	controlsHide = 3 * time.Second
	// frameRetry is how often the loop looks again while a video is due a frame
	// its decoder has not produced yet.
	frameRetry = 8 * time.Millisecond
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
	// audioStop ends the audio pump alone, which is what a track switch needs:
	// the picture keeps playing while the sound is reopened elsewhere.
	audioStop chan struct{}
	audioWG   sync.WaitGroup
	// track is the audio stream the pump is decoding. Only the UI goroutine
	// changes it; the pump is handed its index when it starts.
	track int
	// hardware is the decoder mode the running pump last used, published to the
	// node from the UI goroutine so paint never reads a field another goroutine
	// writes.
	hardware bool

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
	// live is a feed with no length: its decoder is retried after a drop, and
	// the newest frame is preferred over the oldest.
	live bool
	// behind is whether to ask an HLS feed to start behind the live edge; it
	// drops to false when the demuxer refuses the option.
	behind bool
	// hw is whether to ask ffmpeg for hardware decoding. It drops to false when
	// the attempt produced no frame, which is the fallback to software.
	hw bool
	// volume/muted are applied on the next chunk of audio.
	volume float64
	muted  bool

	// startAt is when the player began, shown is whether it has painted a frame
	// yet, and lastFrameAt is when the decoder last delivered one. Together they
	// time the live prefill without giving up on a thin stream.
	startAt     time.Time
	shown       bool
	lastFrameAt time.Time
	// wake asks the frame loop to look again. The pump calls it when a frame
	// lands in an empty queue, so a player waiting for a frame resumes on that
	// frame instead of on the next scheduled tick.
	wake func()
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
		a.loadSubtitles(m)
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
	if media.IsURL(src) || filepath.IsAbs(src) {
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

// probeMedia starts reading the stream properties, so a video knows its size and
// a control bar its length. The probe is an ffmpeg process — a remote stream
// takes a second or more — so it runs in the background and the node shows the
// loading placeholder meanwhile, exactly as a remote image does. A cached src
// is applied straight away.
func (a *App) probeMedia(n tree.Node, m *tree.Media) {
	src := m.Src.Get()
	if src == "" {
		return
	}
	if info, ok := a.mediaInfo[src]; ok {
		a.applyMediaInfo(n, m, src, info)
		return
	}
	path, err := a.resolveMedia(src)
	if err != nil {
		m.LoadErr = err
		m.Loading = false
		return
	}
	m.Loading = true
	m.LoadErr = nil
	a.pulsing = true
	if a.probes == nil {
		a.probes = map[string]bool{}
	}
	if a.probes[src] {
		return // one probe per src, however many nodes share it
	}
	a.probes[src] = true
	go func() {
		info, err := media.Probe(path)
		a.Post(func() { a.finishProbe(src, info, err) })
	}()
}

// finishProbe applies a probe that ran in the background to every node still
// waiting for that src, then starts any player that asked to play early.
func (a *App) finishProbe(src string, info media.Info, err error) {
	delete(a.probes, src)
	if err == nil {
		if a.mediaInfo == nil {
			a.mediaInfo = map[string]media.Info{}
		}
		a.mediaInfo[src] = info
	}
	if a.win == nil || a.win.tree == nil {
		return
	}
	a.win.tree.Walk(func(n tree.Node) {
		m, ok := tree.MediaNode(n)
		if !ok || m.Src.Get() != src || !m.Loading {
			return
		}
		m.Loading = false
		if err != nil {
			m.LoadErr = err
			m.Playing, m.Paused = false, false
			m.Base().MarkDirty(tree.DirtyLayout)
			return
		}
		a.applyMediaInfo(n, m, src, info)
		// A player that was pressed before the probe landed starts now.
		if m.Playing && !m.Paused && a.playerFor(n) == nil {
			if _, perr := a.startPlayer(n); perr != nil {
				m.LoadErr = perr
				m.Playing = false
				m.Base().MarkDirty(tree.DirtyLayout)
			}
		}
	})
	a.markDirty(tree.DirtyLayout)
}

// applyMediaInfo hands one node the probe result: the length, the stream count,
// whether the source is live, and a video's own frame size.
func (a *App) applyMediaInfo(n tree.Node, m *tree.Media, src string, info media.Info) {
	m.Duration = info.Duration
	m.AudioTracks = info.AudioTracks
	m.Live = info.IsLive(src)
	m.Loading = false
	m.Buffering = false
	m.LoadErr = nil
	if v, ok := n.(*tree.Video); ok && info.HasVideo {
		if v.FrameW != info.Width || v.FrameH != info.Height {
			v.Base().MarkDirty(tree.DirtyLayout)
		}
		v.FrameW, v.FrameH = info.Width, info.Height
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
		stop: make(chan struct{}), audioStop: make(chan struct{}),
		track: int(m.AudioTrack.Get()), wake: a.wakeFrames}
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
	live := info.IsLive(src) || m.Live
	p.buffer = bufferSeconds(m.Buffer.Get())
	if live && p.buffer == DefaultBuffer {
		// The design did not ask for a depth, so give a live feed the one its
		// segments need.
		p.buffer = liveDefaultBuffer
	}
	m.Duration = info.Duration
	m.AudioTracks = info.AudioTracks
	m.Live = live
	p.live = live
	p.startAt = time.Now()
	p.behind = p.live
	// A live feed decodes in software: asking for a hardware device costs a
	// driver round trip before the first frame (measured at several seconds with
	// VAAPI) and a live player has no frames to spare. A file can wait.
	p.hw = !p.live
	if !p.live {
		// A live feed has no timeline to start at, so startAt is ignored.
		p.opened = time.Duration(m.StartAt.Get() * float64(time.Second))
	}

	// A video needs a canvas to paint into. It is decoded near the size it is
	// drawn at, so a 4K file in a 480dp box does not decode 8 megapixels a
	// frame.
	if v, ok := n.(*tree.Video); ok && info.HasVideo {
		w, h := decodeSize(info, v.Base().Bounds(), p.live)
		v.FrameW, v.FrameH = w, h
		if v.Data == nil || v.Data.W != w || v.Data.H != h {
			v.Data = &img.Data{W: w, H: h, Pix: make([]byte, w*h*4)}
		}
		p.frame = v.Data
		p.openFrames(w * h * 4)
		// The decoder needs a moment before its first frame; until then the box
		// breathes instead of showing a black rectangle.
		m.Buffering = true
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
//
// A live feed decodes at the drawn size rather than twice it. Its frames are
// what the buffer is made of, so halving each one doubles how many fit a fixed
// byte budget — and that buffer is the difference between riding out a segment
// gap and freezing.
func decodeSize(info media.Info, box tree.Rect, live bool) (int, int) {
	w, h := info.Width, info.Height
	if w <= 0 || h <= 0 {
		return 16, 16
	}
	// Twice the drawn size is enough for a sharp picture at any scale factor.
	detail := 2.0
	if live {
		detail = 1
	}
	maxW, maxH := 1280.0, 1280.0
	if box.W > 0 && box.H > 0 {
		if v := box.W * detail; v < maxW {
			maxW = max(max(v, 64), 2)
		}
		if v := box.H * detail; v < maxH {
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
	budget := maxVideoBufferBytes
	if p.live {
		budget = maxLiveBufferBytes
	}
	if limit := budget / max(frameBytes, 1); n > limit {
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
	p.wg.Add(1)
	go p.pumpVideo()
	p.startAudio(p.opened)
}

// startAudio runs the audio pump from at. A track switch reuses this on the
// same device, so only the sound is reopened.
func (p *mediaPlayer) startAudio(at time.Duration) {
	if p.audio == nil || !p.info.HasAudio {
		return
	}
	p.audioStop = make(chan struct{})
	p.audioWG.Add(1)
	go p.pumpAudio(at, p.track)
}

// pumpVideo decodes frames and queues them; a live feed that drops is reopened
// instead of being taken as the end of a file, and a hardware decoder that
// produced nothing is retried in software.
func (p *mediaPlayer) pumpVideo() {
	defer p.wg.Done()
	if !p.info.HasVideo || p.frames == nil {
		return
	}
	for {
		stopped, frames := p.decodeVideo()
		if stopped {
			return
		}
		if frames == 0 && p.behind {
			// The demuxer refused the start-index option (a non-HLS live format,
			// or an older ffmpeg): play at the edge instead.
			p.behind = false
			continue
		}
		if frames == 0 && p.hw {
			// The device, the driver or the codec refused: software from here
			// on, for this file, without asking again.
			p.hw = false
			continue
		}
		if !p.live {
			return
		}
		select {
		case <-p.stop:
			return
		case <-time.After(liveRetry):
		}
	}
}

// decodeVideo runs one decoder to its end and reports whether the player was
// closed while it ran, plus how many frames it produced.
func (p *mediaPlayer) decodeVideo() (stopped bool, frames int) {
	dec, err := media.OpenVideo(p.src, p.frame.W, p.frame.H, p.opened, p.hw, p.behind)
	if err != nil {
		if !p.hw {
			p.fail(err)
			return true, 0
		}
		return false, 0
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
			switch {
			case p.live:
				// A live feed that drops has not ended; pumpVideo reopens it.
			case err == io.EOF || err == io.ErrUnexpectedEOF:
				p.mu.Lock()
				p.ended = true
				p.mu.Unlock()
			case frames > 0:
				// A decoder that produced frames and then failed is a real
				// failure, not a hardware one to retry.
				p.fail(err)
			}
			return false, frames
		}
		if frames == 0 {
			p.mu.Lock()
			p.hardware = p.hw
			p.mu.Unlock()
		}
		frames++
		p.mu.Lock()
		p.lastFrameAt = time.Now()
		p.mu.Unlock()
		select {
		case <-p.stop:
			return true, frames
		case p.frames <- buf:
		}
		if len(p.frames) == 1 && p.wake != nil {
			p.wake()
		}
	}
}

// pumpAudio streams PCM into the device, staying one buffer ahead. Sleeping
// while the device's queue is full is what keeps a two hour podcast from
// decoding into memory all at once. A live feed is reopened after a drop, the
// way the video pump is.
func (p *mediaPlayer) pumpAudio(at time.Duration, track int) {
	defer p.audioWG.Done()
	if p.audio == nil {
		return
	}
	for {
		if stopped := p.decodeAudio(at, track); stopped || !p.live {
			return
		}
		select {
		case <-p.stop:
			return
		case <-p.audioStop:
			return
		case <-time.After(liveRetry):
		}
	}
}

// decodeAudio feeds one decoder's samples into the device, and reports whether
// the pump was stopped while it ran.
func (p *mediaPlayer) decodeAudio(at time.Duration, track int) (stopped bool) {
	dec, err := media.OpenPCM(p.src, audioRate, audioChannels, at, track)
	if err != nil {
		return true // a silent file is not a failure
	}
	defer dec.Close()
	chunk := make([]byte, 16*1024)
	leadBytes := p.audioLead()
	for {
		if p.haltAudio() {
			return true
		}
		for p.audio.Queued() > leadBytes {
			select {
			case <-p.stop:
				return true
			case <-p.audioStop:
				return true
			case <-time.After(5 * time.Millisecond):
			}
		}
		n, err := dec.Read(chunk)
		if n > 0 {
			p.audio.Queue(volumeScale(chunk[:n], p.volume, p.muted))
		}
		if err != nil {
			return false
		}
	}
}

// haltAudio reports whether the pump should stop: the whole player closed, or
// the audio alone (a track switch, a seek).
func (p *mediaPlayer) haltAudio() bool {
	select {
	case <-p.stop:
		return true
	case <-p.audioStop:
		return true
	default:
		return false
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
	p.haltAudioPump()
	p.wg.Wait()
	if p.audio != nil {
		p.audio.Close()
		p.audio = nil
	}
}

// haltAudioPump stops the audio goroutine, waiting for it so the device is not
// closed underneath a write.
func (p *mediaPlayer) haltAudioPump() {
	select {
	case <-p.audioStop:
	default:
		close(p.audioStop)
	}
	p.audioWG.Wait()
}

// switchAudio reopens the sound at another stream, in place: the picture keeps
// playing and only the decoder changes.
func (p *mediaPlayer) switchAudio(track int, at time.Duration) {
	if p.audio == nil {
		p.track = track
		return
	}
	if track == p.track {
		return
	}
	p.haltAudioPump()
	p.track = track
	p.audio.Flush()
	p.startAudio(at)
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
	case "audioTrack":
		if len(args) != 1 {
			return false
		}
		idx, ok := toFloat(args[0])
		if !ok {
			return false
		}
		a.setMediaAudioTrack(n, int(idx))
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
	if m.Loading {
		// The probe is still running; the player starts when it lands, so a
		// design can autoplay a remote stream without freezing the window.
		m.Playing, m.Paused, m.Ended = true, false, false
		m.Base().MarkDirty(tree.DirtyPaint)
		a.markDirty(tree.DirtyPaint)
		a.needsFrame = true
		return
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
		m.Buffering = false
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
// offers without a custom protocol handler. A live feed has no offsets, so the
// request is ignored.
func (a *App) seekMedia(n tree.Node, at time.Duration) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	if m.Live {
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

// loadSubtitles reads and parses the design's SRT. A local file is a few
// kilobytes read here; a URL is fetched in the background with the same bounded
// client an image uses, so a slow host cannot hold up the window.
func (a *App) loadSubtitles(m *tree.Media) {
	src := m.Subtitles.Get()
	m.RenderedSubtitles = src
	m.Cues, m.SubtitleErr = nil, nil
	if src == "" {
		return
	}
	if isRemoteSrc(src) {
		go func() {
			b, err := fetchRemote(src)
			a.Post(func() { a.finishSubtitles(src, b, err) })
		}()
		return
	}
	b, err := a.readSrc(src)
	a.applySubtitles(m, src, b, err)
}

// finishSubtitles hands a fetched SRT to every player still waiting for it.
func (a *App) finishSubtitles(src string, b []byte, err error) {
	if a.win == nil || a.win.tree == nil {
		return
	}
	a.win.tree.Walk(func(n tree.Node) {
		m, ok := tree.MediaNode(n)
		if !ok || m.Subtitles.Get() != src || m.RenderedSubtitles != src {
			return
		}
		a.applySubtitles(m, src, b, err)
		m.Base().MarkDirty(tree.DirtyPaint)
	})
	a.markDirty(tree.DirtyPaint)
}

// applySubtitles parses one SRT read and stores the result, recording why it
// failed rather than failing the player.
func (a *App) applySubtitles(m *tree.Media, src string, b []byte, err error) {
	if err != nil {
		m.SubtitleErr = err
		return
	}
	cues, perr := subtitle.Parse(b)
	if perr != nil {
		m.SubtitleErr = fmt.Errorf("%s: %w", src, perr)
		return
	}
	m.Cues = cues
}

// setMediaAudioTrack picks an audio stream and reopens just the sound. The
// index is clamped to the streams the file has, so a design that names a track
// the film lacks keeps playing instead of going silent.
func (a *App) setMediaAudioTrack(n tree.Node, track int) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	if track < 0 {
		track = 0
	}
	if m.AudioTracks > 0 && track >= m.AudioTracks {
		track = m.AudioTracks - 1
	}
	m.AudioTrack.Set(float64(track))
	if p := a.playerFor(n); p != nil && m.Playing && !m.Paused {
		p.switchAudio(track, m.Position)
	} else if p != nil {
		p.track = track
	}
	m.Base().MarkDirty(tree.DirtyPaint)
	a.markDirty(tree.DirtyPaint)
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
			m.Loading = false
			m.Live = false
			m.Duration = 0
			m.Position = 0
			m.AudioTracks = 0
			m.Hardware = false
			m.AudioTrack.Set(0)
			m.Playing, m.Paused, m.Ended = false, false, false
			if src != "" {
				a.probeMedia(n, m)
			}
			changed = true
		}
		if m.Subtitles.Get() != m.RenderedSubtitles {
			a.loadSubtitles(m)
			m.Base().MarkDirty(tree.DirtyPaint)
			changed = true
		}
		if p := a.playerFor(n); p != nil {
			p.mu.Lock()
			p.volume, p.muted = clampUnit(m.Volume.Get()), m.Muted.Get()
			p.mu.Unlock()
			// A track the design or Go changed is applied to the running
			// player, without restarting the picture.
			if track := int(m.AudioTrack.Get()); track != p.track {
				a.setMediaAudioTrack(n, track)
				changed = true
			}
		}
		if m.Autoplay.Get() && !m.Playing && !m.Paused && src != "" &&
			m.LoadErr == nil && !m.Loading {
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
		// The control bar follows the pointer: it hides once the pointer has
		// rested on the picture or left it for controlsHide. The remaining time
		// is the loop's wake-up, so a paused player still drops its bar without
		// needing input.
		if m.ControlsShown {
			m.ControlsIdle += time.Duration(dt * float64(time.Second))
			if m.ControlsIdle >= controlsHide {
				m.ControlsShown = false
				m.Base().MarkDirty(tree.DirtyPaint)
				changed = true
			} else if d := controlsHide - m.ControlsIdle; wait == 0 || d < wait {
				wait = d
			}
		}
		// The decoder reports its mode from its own goroutine; the node is
		// written here, on the one goroutine paint reads from.
		p.mu.Lock()
		hw := p.hardware
		p.mu.Unlock()
		if m.Hardware != hw {
			m.Hardware = hw
		}
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
			if p.live {
				// A live feed is paced by the rate it actually delivers, not by
				// the container's claim and not by "as fast as frames arrive":
				// the first drains the buffer (24 fps reported as 25), the second
				// shows a segment in half its duration and then freezes. The
				// deadline is absolute for the same reason a file's is.
				now := time.Now()
				period := p.livePeriod()
				if p.nextFrame.IsZero() {
					if p.waitingForReserve() {
						if wait == 0 || wait > frameRetry {
							wait = frameRetry
						}
						continue
					}
					if p.takeFrame() {
						v.Data.Gen++
						m.Buffering = false
						p.shown = true
						v.Base().MarkDirty(tree.DirtyPaint)
						changed = true
						p.nextFrame = now.Add(period)
					}
				} else if !now.Before(p.nextFrame) {
					if p.takeFrame() {
						v.Data.Gen++
						m.Buffering = false
						p.shown = true
						v.Base().MarkDirty(tree.DirtyPaint)
						changed = true
						next := p.nextFrame.Add(period)
						if next.Before(now.Add(-2 * period)) {
							next = now.Add(period)
						}
						p.nextFrame = next
					}
				}
				if d := time.Until(p.nextFrame); d > 0 {
					if wait == 0 || d < wait {
						wait = d
					}
				} else if wait == 0 || wait > frameRetry {
					// Due but nothing decoded: the pump wakes the loop when a
					// frame lands; this is the fallback for a lost wake-up.
					wait = frameRetry
				}
				if got := time.Duration(float64(p.buffered()) * float64(p.framePeriod())); got != m.Buffered {
					m.Buffered = got
					m.Base().MarkDirty(tree.DirtyPaint)
				}
				continue
			}
			period := p.framePeriod()
			now := time.Now()
			if p.live && !p.shown && p.waitingForReserve() {
				// A feed delivering a segment at a time arrives in bursts. Playing
				// the first frame as soon as it lands leaves no reserve, so every
				// pause between segments shows as a freeze; wait for a cushion
				// first, which is what makes the picture continuous afterwards.
				if wait == 0 || wait > frameRetry {
					wait = frameRetry
				}
				continue
			}
			if p.nextFrame.IsZero() {
				// The first frame is shown as soon as one exists, which is what
				// makes a stall recover without a waiting period.
				if p.takeFrame() {
					v.Data.Gen++
					m.Buffering = false
					p.shown = true
					v.Base().MarkDirty(tree.DirtyPaint)
					changed = true
					p.nextFrame = now.Add(period)
				}
			} else if !now.Before(p.nextFrame) {
				if p.takeFrame() {
					v.Data.Gen++
					m.Buffering = false
					p.shown = true
					v.Base().MarkDirty(tree.DirtyPaint)
					changed = true
					// The deadline moves by exactly one period, not by "one
					// period from now": counting from the tick lets the display
					// rate quantize the stream rate, which is what played a
					// 25 fps film at 21 fps. A deadline more than a period late
					// is a real stall, so the cadence restarts from now instead
					// of sprinting through the frames that were missed.
					next := p.nextFrame.Add(period)
					if next.Before(now.Add(-period)) {
						next = now.Add(period)
					}
					p.nextFrame = next
				}
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
			if d := time.Until(p.nextFrame); d > 0 {
				if wait == 0 || d < wait {
					wait = d
				}
			} else if p.nextFrame.IsZero() || !time.Now().Before(p.nextFrame) {
				// The frame is due but has not decoded yet. The pump wakes the
				// loop the moment it lands; the short fallback keeps a dropped
				// wake-up from freezing the picture.
				if wait == 0 || wait > frameRetry {
					wait = frameRetry
				}
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

// wakeFrames asks for the frame that will draw a decoded picture. It runs on the
// UI goroutine, posted by the pump, and only needs to flag a repaint.
func (a *App) wakeFrames() {
	a.Post(func() {
		a.needsFrame = true
		a.markDirty(tree.DirtyPaint)
	})
}

// waitingForReserve reports whether a live player should keep filling before it
// shows its first frame. Playing the first frame that lands spends the cushion
// the first segment gap would have needed, so the player waits for a couple of
// seconds of picture — unless the feed stalls with something already in hand, in
// which case a thin stream is better than a loading pulse forever.
func (p *mediaPlayer) waitingForReserve() bool {
	if p.frames == nil {
		return false
	}
	want := int(reserveSeconds / p.framePeriod().Seconds())
	if limit := cap(p.frames) / 2; want > limit {
		want = limit
	}
	if want < 1 || len(p.frames) >= want {
		return false
	}
	p.mu.Lock()
	stalled := !p.lastFrameAt.IsZero() && time.Since(p.lastFrameAt) > 2*time.Second
	p.mu.Unlock()
	return !stalled
}

// livePeriod is the frame period a live feed is paced at. The container's rate
// is a starting point, not the truth: a stream that runs a little faster than
// it claims would drain the buffer a frame at a time, and one that runs slower
// would fill it until the picture lagged forever. So the queue depth is the
// feedback — above half means the player is behind and plays faster, below half
// means it is ahead and slows down — which settles the picture at the source's
// real rate without measuring it.
func (p *mediaPlayer) livePeriod() time.Duration {
	period := p.framePeriod()
	if p.frames == nil {
		return period
	}
	// The queue depth is the whole clock. Measuring the decoder's arrival rate
	// cannot work here: the pump is flow-controlled by this very queue, so once
	// it is full the "arrival rate" is the display rate and the measurement
	// chases its own tail. A proportional trim on the depth instead settles at
	// whatever rate keeps the cushion: the container's nominal rate is the
	// starting point (this stream claims 25 and delivers 23.5), and the trim
	// settles a little above it with the queue a little below target, without
	// either drifting.
	target := reserveSeconds / p.framePeriod().Seconds()
	if target < 1 {
		target = 1
	}
	trim := min(max(1+0.8*(target-float64(len(p.frames)))/target, 0.85), 1.5)
	return time.Duration(float64(period) * trim)
}

// framePeriod is how long one frame of the stream stays on screen.
func (p *mediaPlayer) framePeriod() time.Duration {
	fps := p.info.FPS
	if fps <= 0 {
		fps = 30
	}
	return time.Duration(float64(time.Second) / fps)
}

// takeFrame moves a decoded frame into the node's canvas.
func (p *mediaPlayer) takeFrame() bool {
	if p.frames == nil || p.frame == nil {
		return false
	}
	buf, ok := p.nextFrameBuf()
	if !ok || len(buf) != len(p.frame.Pix) {
		return false
	}
	// Swap instead of copying: the decoded buffer becomes the canvas and the
	// old canvas goes back to the pump. Every buffer in the pool is the same
	// size, so this moves a frame with two pointer writes.
	p.frame.Pix, buf = buf, p.frame.Pix
	p.free <- buf
	return true
}

// nextFrameBuf takes the next frame in order. A live feed plays the queue the
// same way a file does and only skips ahead when the queue is nearly full, which
// is what separates a burst from a backlog: an HLS server hands over a whole
// segment at once, and jumping to its last frame turned every burst into a jump
// and every gap into a freeze.
func (p *mediaPlayer) nextFrameBuf() ([]byte, bool) {
	var buf []byte
	select {
	case buf = <-p.frames:
	default:
		return nil, false
	}
	if !p.live {
		return buf, true
	}
	// A queue above the cushion is lag the player should shed near the live
	// edge rather than drift away with. Shedding it in eighths of the excess, one
	// take at a time, is a one-frame skip here and there instead of a jump of a
	// second when the queue hits a threshold.
	target := int(reserveSeconds / p.framePeriod().Seconds())
	if n := len(p.frames); n > target+8 {
		drop := (n - target + 7) / 8
		for i := 0; i < drop; i++ {
			select {
			case next := <-p.frames:
				p.free <- buf
				buf = next
			default:
				return buf, true
			}
		}
	}
	return buf, true
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
