package ui

import (
	"math"

	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

// A theme wipe reveals the new scheme behind a soft edge. Role colours are
// baked into the display list, so the softness is a stack of slices, each
// painted with its own blend of the two schemes: wipeBand is the edge height as
// a fraction of the window and wipeSteps how many slices it is made of.
const (
	wipeBand  = 0.4
	wipeSteps = 48
)

// buildWipeList paints the new scheme above the moving edge, the old one below
// it, and a vertical gradient of blended schemes in between. The tree is
// resolved once per slice, because role colours live in cells; the last pass
// leaves the new scheme in place.
func (a *App) buildWipeList(l *paint.List, tr *tree.Tree, w, h float64) {
	if tr == nil || tr.Root == nil || w <= 0 || h <= 0 {
		return
	}
	oldTheme, newTheme := a.themeFrom, a.themeTo
	if oldTheme == nil {
		oldTheme = newTheme
	}
	if newTheme == nil {
		newTheme = oldTheme
	}
	if oldTheme == nil {
		return
	}

	p := math.Min(math.Max(a.themeWipeP, 0), 1)
	band := h * wipeBand
	// center travels from above the window (all old) to below it (all new).
	center := p*(h+band) - band/2
	softTop := center - band/2
	softBottom := center + band/2

	var ops []paint.Op
	animating := false
	paintWith := func(theme *m3.Theme, clip tree.Rect) {
		a.resolveColorsFor(theme)
		l.Theme = theme
		l.BuildClip(tr, w, h, clip)
		// The list emits every op and lets the renderer clip it; that is fine
		// for one pass, but a soft edge is dozens of them, so drop the ops that
		// cannot touch this slice.
		for _, op := range l.Ops {
			if wipeOpVisible(op) {
				ops = append(ops, op)
			}
		}
		animating = animating || l.Animating
	}

	// The background is one smooth gradient, so the page under the content
	// fades evenly instead of stepping with the sliced content above it.
	topEnd := clampTo(softTop, h)
	bottomStart := clampTo(softBottom, h)
	ops = append(ops, paint.Op{
		Kind: paint.Fill, Shape: paint.CSSGradient,
		Rect:     wipeRect(tree.Rect{W: w, H: h}),
		Clip:     wipeRect(tree.Rect{W: w, H: h}),
		Gradient: wipeGradient(oldTheme, newTheme, softTop, softBottom, h),
	})

	if topEnd > 0 {
		paintWith(newTheme, tree.Rect{W: w, H: topEnd})
	}
	if bottomStart < h {
		paintWith(oldTheme, tree.Rect{Y: bottomStart, W: w, H: h - bottomStart})
	}
	start, end := math.Max(softTop, 0), math.Min(softBottom, h)
	if end > start {
		step := (end - start) / wipeSteps
		for i := 0; i < wipeSteps; i++ {
			y0 := start + float64(i)*step
			y1 := y0 + step
			t := 1 - ((y0+y1)/2-softTop)/band
			paintWith(blendTheme(oldTheme, newTheme, t), tree.Rect{Y: y0, W: w, H: y1 - y0})
		}
	}

	l.Ops = ops
	l.Animating = animating
	l.Theme = newTheme
}

// wipeOpVisible reports whether an op can leave a mark inside its own clip.
// Transformed ops and backdrops are always kept: their geometry is not the
// plain rect, and a backdrop samples the scene around it.
func wipeOpVisible(op paint.Op) bool {
	if op.Rotate != 0 || op.Scale[2] != 0 || op.Kind == paint.Backdrop {
		return true
	}
	c := op.Clip
	if c[2] <= 0 || c[3] <= 0 {
		return false
	}
	pad := 0.0
	switch op.Kind {
	case paint.Shadow:
		pad = math.Abs(float64(op.OffsetX)) + math.Abs(float64(op.OffsetY)) + float64(op.Blur+op.Spread)
	case paint.Text:
		for _, s := range op.TextShadows {
			pad = math.Max(pad, math.Max(math.Abs(s.X), math.Abs(s.Y))+s.Blur)
		}
	}
	x0, y0 := float64(op.Rect[0]), float64(op.Rect[1])
	x1, y1 := x0+float64(op.Rect[2]), y0+float64(op.Rect[3])
	return x1+pad > float64(c[0]) && x0-pad < float64(c[0]+c[2]) &&
		y1+pad > float64(c[1]) && y0-pad < float64(c[1]+c[3])
}

// wipeGradient is the background behind the soft edge: the new surface above,
// the old one below and a straight ramp between. The window can cut the ramp
// while the edge enters or leaves, so the first and last stops take the colour
// the ramp has at the window's own edges instead of the flat end colours.
func wipeGradient(oldTheme, newTheme *m3.Theme, softTop, softBottom, h float64) *tree.Gradient {
	newColor, oldColor := newTheme.Colors.Surface, oldTheme.Colors.Surface
	band := softBottom - softTop
	at := func(y float64) uint32 {
		return blendColor(newColor, oldColor, clampTo((y-softTop)/band, 1))
	}
	stops := []tree.GradientStop{{At: 0, Color: at(0)}}
	if softTop > 0 && softTop < h {
		stops = append(stops, tree.GradientStop{At: softTop / h, Color: newColor})
	}
	if softBottom > 0 && softBottom < h {
		stops = append(stops, tree.GradientStop{At: softBottom / h, Color: oldColor})
	}
	stops = append(stops, tree.GradientStop{At: 1, Color: at(h)})
	return &tree.Gradient{Kind: tree.LinearKind, Angle: 180, Stops: stops}
}

// clampTo keeps a value inside [0, hi].
func clampTo(v, hi float64) float64 {
	return math.Min(math.Max(v, 0), hi)
}

func wipeRect(r tree.Rect) [4]float32 {
	return [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)}
}
