package paint

import (
	"fmt"
	"math"
	"time"

	"github.com/goi-gui/goi/internal/tree"
)

// Media control bar metrics, in dp.
const (
	mediaBarH    = 48.0
	mediaBarPad  = 12.0
	mediaButtonR = 16.0
	mediaTimeW   = 46.0
)

// MediaLayout is where a player's control bar sits. It is a pure function of the
// box, so paint and hit-testing agree without both measuring text.
type MediaLayout struct {
	// Bar is the visible control strip; empty when the design hid it.
	Bar tree.Rect
	// Button is the play/pause circle.
	Button tree.Rect
	// Track is the scrubber's full width, and Thumb the knob on it.
	Track tree.Rect
	Thumb tree.Rect
	// Time is the "0:12 / 1:40" box at the trailing edge.
	Time tree.Rect
}

// MediaLayoutOf lays out the controls of an Audio or Video in its box.
func MediaLayoutOf(m *tree.Media, r tree.Rect) MediaLayout {
	if !m.Controls.Get() || r.W < 120 || r.H < 24 {
		return MediaLayout{}
	}
	bar := tree.Rect{X: r.X, Y: r.Bottom() - mediaBarH, W: r.W, H: mediaBarH}
	if r.H < mediaBarH*2 {
		// A short player puts the bar in the middle of its own box rather than
		// hanging off the bottom.
		bar.Y = r.Y + r.H - mediaBarH
	}
	l := MediaLayout{Bar: bar}
	l.Button = tree.Rect{
		X: bar.X + mediaBarPad,
		Y: bar.Y + (bar.H-2*mediaButtonR)/2,
		W: 2 * mediaButtonR, H: 2 * mediaButtonR,
	}
	l.Time = tree.Rect{X: bar.Right() - mediaBarPad - mediaTimeW, Y: bar.Y,
		W: mediaTimeW, H: bar.H}
	trackX := l.Button.Right() + mediaBarPad
	trackW := l.Time.X - mediaBarPad - trackX
	if trackW < 40 {
		trackW = 40
	}
	l.Track = tree.Rect{X: trackX, Y: bar.Y + bar.H/2 - 2, W: trackW, H: 4}
	frac := 0.0
	if m.Duration > 0 {
		frac = float64(m.Position) / float64(m.Duration)
	}
	frac = math.Max(0, math.Min(1, frac))
	l.Thumb = tree.Rect{X: l.Track.X + frac*l.Track.W - 6, Y: l.Track.Y - 4, W: 12, H: 12}
	return l
}

// MediaSeekFraction turns a press on the track into a 0..1 position, which is
// the value the ui layer seeks to.
func (l MediaLayout) MediaSeekFraction(x float64) (float64, bool) {
	if l.Track.W <= 0 {
		return 0, false
	}
	f := (x - l.Track.X) / l.Track.W
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return f, true
}

// paintMediaContent draws a player's own visuals on top of its box: the video
// frame, the control bar, and the decoder's message when it could not start.
func (l *List) paintMediaContent(m *tree.Media, r tree.Rect, clip tree.Rect) {
	if v, ok := m.Base().Node().(*tree.Video); ok {
		l.paintVideoFrame(v, r, clip)
	}
	if m.LoadErr != nil {
		l.paintMediaError(m, r, clip)
		return
	}
	lay := MediaLayoutOf(m, r)
	if lay.Bar.H == 0 {
		return
	}
	l.paintMediaBar(m, r, lay, clip)
}

// paintVideoFrame draws the current frame of a video into its box.
func (l *List) paintVideoFrame(v *tree.Video, r tree.Rect, clip tree.Rect) {
	if v.Data == nil || v.Data.W <= 0 || v.Data.H <= 0 {
		l.fillRect(r, withAlpha(l.Theme.Colors.SurfaceContainerHighest, 0.5), v.Base().Radii()[0], 0, 0, clip)
		return
	}
	if v.Data.Pix == nil {
		return
	}
	l.paintBitmap(v.Data, v.Fit.Get(), v.Base(), false, 0, r, clip)
}

// paintMediaError writes the decoder's message, which is what a missing ffmpeg
// or an unreadable file looks like in the window.
func (l *List) paintMediaError(m *tree.Media, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	st := l.style("bodySmall")
	msg := m.LoadErr.Error()
	box := tree.Rect{X: r.X + 12, Y: r.Y, W: r.W - 24, H: r.H}
	l.fillRect(r, withAlpha(col.ErrorContainer, 0.5), m.Base().Radii()[0], 0, 0, clip)
	l.textIn(msg, st, col.OnErrorContainer, box, AlignCenter, clip)
}

// paintMediaBar draws the strip: a translucent scrim, the play button, the
// scrubber and the time.
func (l *List) paintMediaBar(m *tree.Media, r tree.Rect, lay MediaLayout, clip tree.Rect) {
	col := l.Theme.Colors
	// A video draws the bar over its picture, so it needs a scrim to stay
	// readable; an audio element is a surface of its own.
	if _, isVideo := m.Base().Node().(*tree.Video); isVideo {
		l.fillRect(lay.Bar, withAlpha(col.Scrim, 0.45), 0, 0, 0, intersect(clip, lay.Bar))
	} else {
		l.fillRect(lay.Bar, withAlpha(col.SurfaceContainerHighest, 0.7), 0, 0, 0,
			intersect(intersect(clip, r), lay.Bar))
	}
	icon := "play_arrow"
	if m.Playing && !m.Paused {
		icon = "pause"
	} else if m.Ended {
		icon = "replay"
	}
	size := 2 * mediaButtonR
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Circle,
		Rect:  rectArr(lay.Button),
		Color: withAlpha(col.Primary, 0.18), Clip: clipArr(clip),
	})
	l.iconIn(icon, size*0.62, col.OnSurface,
		lay.Button.X+lay.Button.W/2, lay.Button.Y+lay.Button.H/2, clip)

	track := withAlpha(col.OnSurface, 0.24)
	l.fillRect(lay.Track, track, lay.Track.H/2, 0, 0, clip)
	frac := 0.0
	if m.Duration > 0 {
		frac = math.Max(0, math.Min(1, float64(m.Position)/float64(m.Duration)))
	}
	if frac > 0 {
		l.fillRect(tree.Rect{X: lay.Track.X, Y: lay.Track.Y, W: lay.Track.W * frac, H: lay.Track.H},
			col.Primary, lay.Track.H/2, 0, 0, clip)
	}
	l.fillRect(lay.Thumb, col.Primary, lay.Thumb.W/2, 0, 0, clip)

	st := l.style("labelSmall")
	l.textIn(MediaClock(m.Position)+" / "+MediaClock(m.Duration), st, col.OnSurface,
		lay.Time, AlignEnd, clip)
}

// MediaClock writes a duration as m:ss, or h:mm:ss past an hour.
func MediaClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	h, m, s := total/3600, (total/60)%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
