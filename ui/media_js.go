//go:build js

package ui

// Media in the browser uses the page's own decoder: a hidden <video> or <audio>
// plays the file and the app copies its frames into a bitmap. There is no
// ffmpeg, no probe and no decoder goroutine — the browser already has the
// container support, the hardware decoding and the audio device.
//
// The frame loop still owns the picture: it asks the element for the frame at
// the media's current time and packs it into the node's image, so paint, hover,
// the control bar and fullscreen behave the way they do on the desktop.

import (
	"fmt"
	"syscall/js"
	"time"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/tree"
)

// mediaPlayer is one page element playing one node.
type mediaPlayer struct {
	node tree.Node
	m    *tree.Media

	el js.Value
	// video is the <video> element, or an undefined js.Value for an Audio.
	video js.Value
	// canvas and ctx are the offscreen 2D surface the frame is read from; a
	// canvas is the only way to turn a video frame into pixels in a browser.
	canvas js.Value
	ctx    js.Value

	// src is the src the element was given, so a design change restarts it.
	src string
	// width and height are the video's own size, from its metadata.
	width, height int
	// lastFrame is the time of the frame packed into the node, so a paused
	// player is not copied again every tick.
	lastFrame time.Duration
	// volume and muted are mirrored to the element when they change.
	volume float64
	muted  bool
}

// loadMedia attaches an element to every Audio and Video in a freshly built
// tree. Nothing starts playing: an element with no `autoplay` waits for Play,
// which is what keeps a page of paused players silent.
func (a *App) loadMedia(tr *tree.Tree) {
	a.hasMedia = false
	a.media = a.media[:0]
	a.decoders = mediaCache{}
	if tr == nil {
		return
	}
	for _, n := range tr.All() {
		m, ok := tree.MediaNode(n)
		if !ok {
			continue
		}
		a.hasMedia = true
		m.RenderedSrc = m.Src.Get()
		a.loadSubtitles(m)
		if m.Src.Get() == "" {
			continue
		}
		if _, err := a.startPlayer(n); err != nil {
			m.LoadErr = err
			m.Loading = false
		}
	}
}

// startPlayer creates the element for a node and loads the src into it.
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
	url, err := a.mediaURL(src)
	if err != nil {
		return nil, err
	}
	p := &mediaPlayer{node: n, m: m, src: src, volume: clampUnit(m.Volume.Get()), muted: m.Muted.Get()}
	if _, isVideo := n.(*tree.Video); isVideo {
		p.video = js.Global().Get("document").Call("createElement", "video")
		p.el = p.video
		p.el.Set("playsInline", true)
		p.canvas = js.Global().Get("document").Call("createElement", "canvas")
		p.ctx = p.canvas.Call("getContext", "2d")
	} else {
		p.el = js.Global().Get("document").Call("createElement", "audio")
	}
	p.el.Set("preload", "auto")
	p.el.Set("src", url)
	p.el.Set("volume", p.volume)
	p.el.Set("muted", p.muted)
	// The browser reports the length and the picture size once it has metadata,
	// which is the probe the desktop runs against ffmpeg.
	loaded := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		a.Post(func() { a.applyElementInfo(p) })
		return nil
	})
	p.el.Call("addEventListener", "loadedmetadata", loaded)
	failed := js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "the browser could not play this source"
		if len(args) > 0 {
			if e := args[0].Get("target").Get("error"); !e.IsNull() && !e.IsUndefined() {
				msg = e.Get("message").String()
			}
		}
		a.Post(func() {
			m.LoadErr = fmt.Errorf("%s: %s", src, msg)
			m.Loading = false
			m.Playing, m.Paused = false, false
			m.Base().MarkDirty(tree.DirtyLayout)
		})
		return nil
	})
	p.el.Call("addEventListener", "error", failed)
	m.Loading = true
	m.LoadErr = nil
	a.media = append(a.media, p)
	if m.Playing && !m.Paused {
		p.el.Call("play")
	}
	return p, nil
}

// applyElementInfo copies what the element knows onto the node.
func (a *App) applyElementInfo(p *mediaPlayer) {
	m := p.m
	m.Loading = false
	m.Buffering = false
	m.LoadErr = nil
	if d := p.el.Get("duration"); !d.IsNaN() && d.Float() > 0 {
		m.Duration = time.Duration(d.Float() * float64(time.Second))
	}
	m.Live = p.el.Get("duration").IsNaN()
	if v, ok := p.node.(*tree.Video); ok && p.video.Truthy() {
		p.width = p.video.Get("videoWidth").Int()
		p.height = p.video.Get("videoHeight").Int()
		if p.width > 0 && p.height > 0 && (v.FrameW != p.width || v.FrameH != p.height) {
			v.FrameW, v.FrameH = p.width, p.height
			v.Base().MarkDirty(tree.DirtyLayout)
		}
	}
	m.Base().MarkDirty(tree.DirtyLayout)
	a.markDirty(tree.DirtyLayout)
}

// mediaURL turns a src into something an element can load: a URL as written, an
// asset or picked file as a blob URL built from its bytes.
func (a *App) mediaURL(src string) (string, error) {
	if isRemoteSrc(src) || isBlobURL(src) {
		return src, nil
	}
	b, err := a.readSrc(src)
	if err != nil {
		return "", err
	}
	u8 := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(u8, b)
	blob := js.Global().Get("Blob").New([]any{u8}, map[string]any{"type": mimeOf(src)})
	return js.Global().Get("URL").Call("createObjectURL", blob).String(), nil
}

// isBlobURL reports whether src is already an object URL.
func isBlobURL(src string) bool {
	return len(src) >= 5 && src[:5] == "blob:"
}

// mimeOf guesses a container type from the file's extension, which a Blob needs
// for the browser to pick a decoder.
func mimeOf(src string) string {
	i := len(src)
	for j := len(src) - 1; j >= 0; j-- {
		if src[j] == '.' {
			i = j
			break
		}
		if src[j] == '/' {
			break
		}
	}
	switch src[i:] {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogv":
		return "video/ogg"
	case ".mov":
		return "video/quicktime"
	case ".mkv":
		return "video/x-matroska"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	}
	return "application/octet-stream"
}

// playerFor returns the element playing a node.
func (a *App) playerFor(n tree.Node) *mediaPlayer {
	for _, p := range a.media {
		if p.node == n {
			return p
		}
	}
	return nil
}

// dropPlayer forgets a player and releases its element.
func (a *App) dropPlayer(n tree.Node) {
	for i, p := range a.media {
		if p.node != n {
			continue
		}
		a.media = append(a.media[:i], a.media[i+1:]...)
		p.el.Call("pause")
		return
	}
}

// playMedia starts or resumes playback.
func (a *App) playMedia(n tree.Node) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	if m.Ended {
		a.seekMedia(n, 0)
	}
	m.Playing, m.Paused, m.Ended = true, false, false
	p := a.playerFor(n)
	if p == nil {
		// The element may still be loading; it starts on the metadata event.
		if _, err := a.startPlayer(n); err != nil {
			m.LoadErr = err
			m.Playing = false
		}
		a.markDirty(tree.DirtyLayout)
		return
	}
	p.el.Call("play")
	m.Base().MarkDirty(tree.DirtyPaint)
	a.markDirty(tree.DirtyLayout)
}

// pauseMedia holds the picture where it is.
func (a *App) pauseMedia(n tree.Node) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	m.Paused = true
	if p := a.playerFor(n); p != nil {
		p.el.Call("pause")
	}
	m.Base().MarkDirty(tree.DirtyPaint)
	a.markDirty(tree.DirtyLayout)
}

// stopMedia rewinds and holds.
func (a *App) stopMedia(n tree.Node) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	a.seekMedia(n, 0)
	m.Playing, m.Paused, m.Ended = false, false, false
	if p := a.playerFor(n); p != nil {
		p.el.Call("pause")
	}
	m.Base().MarkDirty(tree.DirtyPaint)
	a.markDirty(tree.DirtyLayout)
}

// seekMedia jumps to a position.
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
	m.Position = at
	if p := a.playerFor(n); p != nil {
		p.el.Set("currentTime", at.Seconds())
		p.lastFrame = -1
	}
	m.Seeked.Emit(at)
	m.Base().MarkDirty(tree.DirtyPaint)
	a.markDirty(tree.DirtyLayout)
}

// setMediaAudioTrack is a no-op in a page: the element picks a default audio
// track and a page cannot switch an embedded one, so the property is recorded
// and the first track keeps playing.
func (a *App) setMediaAudioTrack(n tree.Node, track int) {
	if m, ok := tree.MediaNode(n); ok && track >= 0 {
		m.AudioTrack.Set(float64(track))
	}
}

// setMediaVolume applies a new volume, clamped to 0..1.
func (a *App) setMediaVolume(n tree.Node, v float64) {
	m, ok := tree.MediaNode(n)
	if !ok {
		return
	}
	m.Volume.Set(clampUnit(v))
	if p := a.playerFor(n); p != nil {
		p.volume = clampUnit(v)
		p.el.Set("volume", p.volume)
	}
}

// syncMedia follows the design: a src change reloads the element, a muted or
// volume change is applied, and an autoplay node starts on the frame it appears.
func (a *App) syncMedia() bool {
	if a.win == nil || a.win.tree == nil || !a.hasMedia {
		return false
	}
	changed := false
	a.win.tree.Walk(func(n tree.Node) {
		m, ok := tree.MediaNode(n)
		if !ok {
			return
		}
		if src := m.Src.Get(); src != "" && src != m.RenderedSrc {
			if p := a.playerFor(n); p != nil {
				a.dropPlayer(n)
			}
			m.RenderedSrc = src
			m.Position, m.Duration, m.Ended = 0, 0, false
			m.Loading = true
			if _, err := a.startPlayer(n); err != nil {
				m.LoadErr = err
				m.Loading = false
			}
			if a.syncSubtitles(m) {
				changed = true
			}
			changed = true
			return
		}
		p := a.playerFor(n)
		if p == nil {
			return
		}
		if vol := clampUnit(m.Volume.Get()); vol != p.volume {
			p.volume = vol
			p.el.Set("volume", vol)
			changed = true
		}
		if muted := m.Muted.Get(); muted != p.muted {
			p.muted = muted
			p.el.Set("muted", muted)
			changed = true
		}
		// Autoplay is what starts a player that loaded without being pressed.
		if m.Autoplay.Get() && !m.Playing && !m.Paused && m.LoadErr == nil && !m.Loading {
			a.playMedia(n)
			changed = true
			return
		}
		if m.Playing && !m.Paused && p.el.Get("paused").Bool() {
			p.el.Call("play")
		}
		if !m.Playing && !p.el.Get("paused").Bool() && m.Paused {
			p.el.Call("pause")
		}
	})
	return changed
}

// syncSubtitles reloads the captions when a design changes them.
func (a *App) syncSubtitles(m *tree.Media) bool {
	if m.Subtitles.Get() == m.RenderedSubtitles {
		return false
	}
	a.loadSubtitles(m)
	return true
}

// tickMedia advances the position from the element's own clock and packs the
// video frame that is due.
func (a *App) tickMedia(dt float64) (bool, time.Duration) {
	if len(a.media) == 0 {
		return false, 0
	}
	var changed bool
	var wait time.Duration
	for i := 0; i < len(a.media); i++ {
		p := a.media[i]
		m := p.m
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
		if !m.Playing || m.Paused {
			continue
		}
		if p.el.Get("ended").Bool() {
			m.Playing, m.Paused, m.Ended = false, false, true
			if m.Duration > 0 {
				m.Position = m.Duration
			}
			m.End.Emit(m.Duration)
			m.Base().MarkDirty(tree.DirtyPaint)
			changed = true
			continue
		}
		if m.Loop.Get() && m.Duration > 0 && p.el.Get("currentTime").Float() >= m.Duration.Seconds() {
			a.seekMedia(p.node, 0)
			p.el.Call("play")
			changed = true
			continue
		}
		// The element is the clock: copying its time each tick is what keeps the
		// scrubber honest when the browser stalls or buffers.
		pos := time.Duration(p.el.Get("currentTime").Float() * float64(time.Second))
		if pos != m.Position {
			m.Position = pos
			m.Base().MarkDirty(tree.DirtyPaint)
			changed = true
		}
		if buffering := p.el.Get("readyState").Int() < 3; m.Buffering != buffering {
			m.Buffering = buffering
			m.Base().MarkDirty(tree.DirtyPaint)
			changed = true
		}
		if v, ok := p.node.(*tree.Video); ok && p.video.Truthy() {
			if p.grabFrame(v, pos) {
				m.Base().MarkDirty(tree.DirtyPaint)
				changed = true
			}
		}
		// A playing video wants the next display refresh; audio sleeps until the
		// clock changes enough to matter.
		if wait == 0 || wait > frameRetry {
			wait = frameRetry
		}
	}
	return changed, wait
}

// grabFrame copies the video's current frame into the node's bitmap, once per
// decoded frame.
func (p *mediaPlayer) grabFrame(v *tree.Video, at time.Duration) bool {
	if p.width <= 0 || p.height <= 0 {
		return false
	}
	if at == p.lastFrame && v.Data != nil {
		return false
	}
	if p.canvas.Get("width").Int() != p.width || p.canvas.Get("height").Int() != p.height {
		p.canvas.Set("width", p.width)
		p.canvas.Set("height", p.height)
	}
	p.ctx.Call("drawImage", p.video, 0, 0, p.width, p.height)
	data := p.ctx.Call("getImageData", 0, 0, p.width, p.height).Get("data")
	pix := make([]byte, p.width*p.height*4)
	js.CopyBytesToGo(pix, data)
	// getImageData is RGBA, which is the atlas' own order.
	if v.Data == nil || v.Data.W != p.width || v.Data.H != p.height {
		v.Data = &img.Data{W: p.width, H: p.height, Pix: pix}
	} else {
		v.Data.Pix = pix
	}
	v.Data.Gen++
	p.lastFrame = at
	return true
}

// closeMedia stops every element. The object URLs stay for the run, so a hot
// reload does not have to build them again.
func (a *App) closeMedia() {
	for _, p := range a.media {
		p.el.Call("pause")
	}
	a.media = a.media[:0]
}
