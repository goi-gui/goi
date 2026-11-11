package ui

import (
	"fmt"
	"time"

	"github.com/goi-gui/goi/internal/media"
	"github.com/goi-gui/goi/internal/tree"
)

// The media API, for Go. A design reaches the same operations by calling the
// methods on an element's id (`clip.play()`), so both sides do one thing.
//
// Every call may be made from Bind or from an App.Post callback: it touches the
// tree and the players, which live on the UI goroutine.

// PlayMedia starts or resumes an Audio or Video.
func (a *App) PlayMedia(n Node) { a.playMedia(n) }

// PauseMedia pauses a player, keeping its position.
func (a *App) PauseMedia(n Node) { a.pauseMedia(n) }

// StopMedia rewinds a player and holds it at the start.
func (a *App) StopMedia(n Node) { a.stopMedia(n) }

// ToggleMedia starts a paused player and pauses a playing one.
func (a *App) ToggleMedia(n Node) {
	if m, ok := tree.MediaNode(n); ok && m.Playing && !m.Paused {
		a.pauseMedia(n)
		return
	}
	a.playMedia(n)
}

// SeekMedia jumps to a position. The decoder restarts there, which is the only
// seek ffmpeg offers without a custom protocol handler.
func (a *App) SeekMedia(n Node, at time.Duration) { a.seekMedia(n, at) }

// SetMediaVolume sets a player's volume, clamped to 0..1.
func (a *App) SetMediaVolume(n Node, v float64) { a.setMediaVolume(n, v) }

// MediaPosition returns where a player is, and how long its file runs.
func MediaPosition(n Node) (at, total time.Duration, ok bool) {
	m, is := tree.MediaNode(n)
	if !is {
		return 0, 0, false
	}
	return m.Position, m.Duration, true
}

// IsPlayingMedia reports whether n is a player that is currently running.
func IsPlayingMedia(n Node) bool {
	m, ok := tree.MediaNode(n)
	return ok && m.Playing && !m.Paused
}

// MediaAvailable reports whether an ffmpeg binary was found. A design that needs
// a player can show a fallback when it is false, and it is what a settings page
// reads.
func MediaAvailable() bool { return media.Available() }

// MediaError returns why a player could not start: a missing ffmpeg, an
// unreadable file, or a codec ffmpeg cannot decode.
func MediaError(n Node) error {
	m, ok := tree.MediaNode(n)
	if !ok {
		return fmt.Errorf("ui: %s is not a media element", n.Kind())
	}
	return m.LoadErr
}
