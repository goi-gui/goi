package ui

// The parts of media playback that do not depend on a decoder: the methods a
// design calls through the bridge, fullscreen, subtitles, and the two unit
// conversions. The decoder itself is ffmpeg on the desktop (media.go) and the
// browser's own media stack in a page (media_js.go).

import (
	"fmt"
	"time"

	"github.com/goi-gui/goi/internal/subtitle"
	"github.com/goi-gui/goi/internal/tree"
)

// mediaCursorIdle hides the pointer once a video's controls rest, which is the
// same three seconds that hide the bar. A driver-less build has no pointer.
func (a *App) mediaCursorIdle(n tree.Node) {
	if d, ok := a.driver.(*gpuDriver); ok && d.input != nil {
		d.input.hideMediaCursor(n)
	}
}

const (
	// DefaultBuffer is how far ahead a player decodes when the design does not
	// say. Long enough to ride out a hiccup, short enough to seek quickly.
	DefaultBuffer = 1.5
	// controlsHide is how long the control bar stays after the pointer stops
	// moving over a player, or leaves it.
	controlsHide = 3 * time.Second
	// frameRetry is how often the loop looks again while a video is due a frame
	// its decoder has not produced yet.
	frameRetry = 8 * time.Millisecond
)

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
