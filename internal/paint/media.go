package paint

import (
	"fmt"
	"math"
	"time"

	"github.com/goi-gui/goi/internal/subtitle"
	"github.com/goi-gui/goi/internal/text"
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

// MediaLayoutOf lays out the controls of an Audio or Video in its box. A bar
// that auto-hid lays out as empty, so paint and hit-testing agree there is
// nothing to press until the pointer comes back.
func MediaLayoutOf(m *tree.Media, r tree.Rect) MediaLayout {
	if !m.Controls.Get() || !m.ControlsShown || r.W < 120 || r.H < 24 {
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
	if m.Live {
		// There is no timeline to scrub, so a live bar has no track and the
		// time box becomes the LIVE badge.
		return l
	}
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
	// A fullscreen video covers the window, so Build paints it last instead of
	// where its own box sits.
	if v, ok := m.Base().Node().(*tree.Video); ok && v.Fullscreen.Get() {
		return
	}
	l.paintMediaFrame(m, r, clip)
}

// paintMediaFrame draws a player at a given rect, whatever its fullscreen state.
// Build uses it for the overlay pass.
func (l *List) paintMediaFrame(m *tree.Media, r tree.Rect, clip tree.Rect) {
	if v, ok := m.Base().Node().(*tree.Video); ok {
		l.paintVideoFrame(v, r, clip)
		l.paintSubtitle(m, r, clip)
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
		// A player still waiting for its probe breathes, so the space reads as
		// loading instead of broken, the same wash a remote image gets.
		if v.Loading || v.Buffering {
			alpha := 0.06 + 0.10*v.Phase
			l.fillRect(r, withAlpha(l.Theme.Colors.OnSurface, alpha), v.Base().Radii()[0], 0, 0, clip)
			return
		}
		l.fillRect(r, withAlpha(l.Theme.Colors.SurfaceContainerHighest, 0.5), v.Base().Radii()[0], 0, 0, clip)
		return
	}
	if v.Data.Pix == nil {
		return
	}
	l.paintBitmap(v.Data, v.Fit.Get(), v.Base(), false, 0, r, clip)
}

// paintSubtitle draws the caption playing now: centered near the bottom of the
// picture, just above the control bar when it is showing. The defaults are white
// on a dark scrim in every theme, because the picture, not the scheme, is what
// the text sits on; a design can recolour both, resize the text, thicken it and
// give it a shadow.
func (l *List) paintSubtitle(m *tree.Media, r tree.Rect, clip tree.Rect) {
	text := subtitle.At(m.Cues, m.Position)
	if text == "" || r.W <= 0 {
		return
	}
	st := l.style("bodyMedium")
	if v := m.SubtitleFontSize.Get(); v > 0 {
		st.Size = v
		st.LineHeight = v * 1.2
	}
	st.Weight = subtitleWeight(m, st.Weight)
	boxW := math.Min(r.W*0.86, 760)
	lines := l.wrapLines(text, boxW, st)
	if len(lines) == 0 {
		return
	}
	color := m.SubtitleColor.Get()
	if color == 0 {
		color = 0xFFFFFFFF
	}
	plate := m.SubtitleBackground.Get()
	shadow := l.subtitleShadow(m)
	lh := st.Size * 1.3
	bottom := r.Bottom() - 12
	if m.Controls.Get() && m.ControlsShown && r.W >= 120 && r.H >= 24 {
		bottom = r.Bottom() - mediaBarH - 8
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i].Text
		if line == "" {
			continue
		}
		w := textWidth(l, line, st)
		y := bottom - float64(len(lines)-i)*lh
		if y < r.Y {
			break
		}
		box := tree.Rect{X: r.X + (r.W-boxW)/2, Y: y, W: boxW, H: lh}
		if plate != 0 {
			l.fillRect(tree.Rect{X: box.X + (boxW-w)/2 - 8, Y: y, W: w + 16, H: lh},
				plate, 4, 0, 0, clip)
		}
		before := len(l.Ops)
		l.textIn(line, st, color, box, AlignCenter, clip)
		if len(shadow) > 0 && len(l.Ops) > before {
			l.Ops[len(l.Ops)-1].TextShadows = shadow
		}
	}
}

// subtitleWeight resolves subtitleFontWeight against the role's weight, so the
// CSS keywords (`bold`, `bolder`, `lighter`) work here the way they do on Text.
func subtitleWeight(m *tree.Media, role int) int {
	switch w := int(m.SubtitleFontWeight.Get()); {
	case w == 0:
		return role
	case w < 0:
		// bolder and lighter are relative: -1 steps the role up, -2 down.
		return text.StepWeight(role, w == -1)
	}
	return int(m.SubtitleFontWeight.Get())
}

// subtitleShadow turns the caption's shadow layers into paint layers, resolving
// a role colour against the scheme.
func (l *List) subtitleShadow(m *tree.Media) []tree.ShadowLayer {
	all := m.SubtitleShadow()
	if len(all) == 0 {
		return nil
	}
	out := make([]tree.ShadowLayer, 0, len(all))
	for _, s := range all {
		if s.Inset {
			continue
		}
		if s.Color == 0 && s.Role != "" && l.Theme != nil {
			if c, ok := l.Theme.Colors.Role(s.Role); ok {
				s.Color = c
			}
		}
		s.Color = shadowColor(s, l.Theme)
		out = append(out, s)
	}
	return out
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
	// A recoloured bar keeps its shape: the text, the icons and the scrubber each
	// fall back to the theme's own colour when the design left them at 0.
	textColor := m.ControlsTextColor.Get()
	if textColor == 0 {
		textColor = col.OnSurface
	}
	iconColor := m.ControlsIconColor.Get()
	if iconColor == 0 {
		iconColor = col.OnSurface
	}
	trackColor := m.ControlsTrackColor.Get()
	playedColor := col.Primary
	if trackColor != 0 {
		playedColor = trackColor
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
	l.iconIn(icon, size*0.62, iconColor,
		lay.Button.X+lay.Button.W/2, lay.Button.Y+lay.Button.H/2, clip)

	st := l.style("labelSmall")
	if m.Live {
		// A live feed has no timeline: a red dot and the word say it is at the
		// edge, which is what a scrubber cannot.
		dot := tree.Rect{X: lay.Time.X, Y: lay.Bar.Y + lay.Bar.H/2 - 4, W: 8, H: 8}
		l.fillRect(dot, col.Error, 4, 0, 0, clip)
		l.textIn("LIVE", st, textColor,
			tree.Rect{X: lay.Time.X + 12, Y: lay.Time.Y, W: lay.Time.W, H: lay.Time.H},
			AlignStart, clip)
		return
	}
	track := withAlpha(col.OnSurface, 0.24)
	if trackColor != 0 {
		track = withAlpha(trackColor, 0.32)
	}
	l.fillRect(lay.Track, track, lay.Track.H/2, 0, 0, clip)
	frac := 0.0
	if m.Duration > 0 {
		frac = math.Max(0, math.Min(1, float64(m.Position)/float64(m.Duration)))
	}
	// The buffered span sits behind the played part, so a reader can see how far
	// ahead the decoder is before the picture stalls.
	if m.Buffered > 0 && m.Duration > 0 {
		end := math.Min(1, frac+float64(m.Buffered)/float64(m.Duration))
		buffered := withAlpha(col.OnSurface, 0.42)
		if trackColor != 0 {
			buffered = withAlpha(trackColor, 0.55)
		}
		l.fillRect(tree.Rect{X: lay.Track.X, Y: lay.Track.Y, W: lay.Track.W * end, H: lay.Track.H},
			buffered, lay.Track.H/2, 0, 0, clip)
	}
	if frac > 0 {
		l.fillRect(tree.Rect{X: lay.Track.X, Y: lay.Track.Y, W: lay.Track.W * frac, H: lay.Track.H},
			playedColor, lay.Track.H/2, 0, 0, clip)
	}
	l.fillRect(lay.Thumb, playedColor, lay.Thumb.W/2, 0, 0, clip)

	l.textIn(MediaClock(m.Position)+" / "+MediaClock(m.Duration), st, textColor,
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
