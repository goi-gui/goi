package paint

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// A colour picker draws its square, hue bar and preview itself: like the date
// and time pickers, its cells are not nodes, so the geometry lives here and the
// input layer reads it back through ColorLayoutOf.
const (
	colorPad      = 24.0
	colorTitleTop = 16.0
	colorValueTop = 36.0
	colorSVTop    = 72.0
	colorSVH      = 140.0
	colorHueTop   = 224.0
	colorHueH     = 12.0
	colorFootTop  = 248.0
	colorSwatch   = 40.0
)

// ColorLayout is where a ColorPicker draws its square, hue bar and preview.
type ColorLayout struct {
	SV     tree.Rect // the saturation/value square
	Hue    tree.Rect // the hue bar
	Swatch tree.Rect // the round preview
	Hex    tree.Rect // the value text beside it
}

// ColorLayoutOf places the controls inside the picker's box. It is a pure
// function of the box, so paint and hit testing cannot drift.
func ColorLayoutOf(r tree.Rect) ColorLayout {
	w := r.W - 2*colorPad
	return ColorLayout{
		SV:     tree.Rect{X: r.X + colorPad, Y: r.Y + colorSVTop, W: w, H: colorSVH},
		Hue:    tree.Rect{X: r.X + colorPad, Y: r.Y + colorHueTop, W: w, H: colorHueH},
		Swatch: tree.Rect{X: r.X + colorPad, Y: r.Y + colorFootTop, W: colorSwatch, H: colorSwatch},
		Hex: tree.Rect{
			X: r.X + colorPad + colorSwatch + 12,
			Y: r.Y + colorFootTop,
			W: w - colorSwatch - 12,
			H: colorSwatch,
		},
	}
}

// SVAt returns the saturation and value at a point, clamped to the square so a
// drag that leaves the box still picks the edge.
func (c ColorLayout) SVAt(x, y float64) (s, v float64) {
	if c.SV.W <= 0 || c.SV.H <= 0 {
		return 0, 0
	}
	s = (x - c.SV.X) / c.SV.W
	v = 1 - (y-c.SV.Y)/c.SV.H
	return clamp01(s), clamp01(v)
}

// HueAt returns the hue at a point on the bar, clamped to its ends.
func (c ColorLayout) HueAt(x float64) float64 {
	if c.Hue.W <= 0 {
		return 0
	}
	return clamp01((x-c.Hue.X)/c.Hue.W) * 360
}

func (l *List) paintColorPicker(c *tree.Component, r tree.Rect, clip tree.Rect) {
	cp, ok := c.Node().(*tree.ColorPicker)
	if !ok {
		return
	}
	col := l.Theme.Colors
	l.shadow(r, m3.ShapeExtraLarge, m3.Elevation3, clip)
	l.fillRect(r, col.SurfaceContainerHigh, m3.ShapeExtraLarge, 0, 0, clip)

	h, s, v := cp.HSV()
	hex := cp.Hex()
	label := c.Label.Get()
	if label == "" {
		label = "Select colour"
	}
	inner := tree.Rect{X: r.X + colorPad, Y: r.Y + colorTitleTop, W: r.W - 2*colorPad, H: 16}
	l.textIn(label, l.style("labelMedium"), col.OnSurfaceVariant, inner, AlignStart, clip)
	l.textIn(hex, l.style("titleMedium"), col.OnSurface,
		tree.Rect{X: r.X + colorPad, Y: r.Y + colorValueTop, W: r.W - 2*colorPad, H: 28},
		AlignStart, clip)

	cl := ColorLayoutOf(r)

	// The square is the pure hue, then white from the left (saturation) and
	// black from the bottom (value). Both fades are the renderer's built-in
	// gradient, so the square needs no texture.
	l.fillRect(cl.SV, tree.HSVColor(h, 1, 1), 12, 0, 0, clip)
	l.gradient(cl.SV, 0xFFFFFFFF, gradRight, 12, clip)
	l.gradient(cl.SV, 0x000000FF, gradUp, 12, clip)

	// The picked point: a white ring with a dark outer edge, visible on both a
	// pale corner and a saturated one.
	mx := clampf(cl.SV.X+s*cl.SV.W, cl.SV.X+9, cl.SV.Right()-9)
	my := clampf(cl.SV.Y+(1-v)*cl.SV.H, cl.SV.Y+9, cl.SV.Bottom()-9)
	l.fillRect(tree.Rect{X: mx - 10, Y: my - 10, W: 20, H: 20}, 0, 10, 0x000000FF, 1, clip)
	l.fillRect(tree.Rect{X: mx - 9, Y: my - 9, W: 18, H: 18}, 0, 9, 0xFFFFFFFF, 2, clip)
	l.fillRect(cl.SV, 0, 12, col.OutlineVariant, 1, clip)

	l.fillShape(cl.Hue, 0, hueGradient(), [4]float64{6, 6, 6, 6}, 0, 0, clip)
	hx := clampf(cl.Hue.X+h/360*cl.Hue.W, cl.Hue.X+8, cl.Hue.Right()-8)
	hy := cl.Hue.Y + cl.Hue.H/2
	l.fillRect(tree.Rect{X: hx - 9, Y: hy - 9, W: 18, H: 18}, 0, 9, 0x000000FF, 1, clip)
	l.fillRect(tree.Rect{X: hx - 8, Y: hy - 8, W: 16, H: 16}, 0, 8, 0xFFFFFFFF, 2, clip)

	l.fillRect(cl.Swatch, tree.HSVColor(h, s, v), colorSwatch/2, col.OutlineVariant, 1, clip)
	l.textIn(hex, l.style("titleMedium"), col.OnSurface, cl.Hex, AlignStart, clip)
}

// hueGradient is the spectrum the hue bar shows, red at both ends.
func hueGradient() *tree.Gradient {
	stops := make([]tree.GradientStop, 7)
	for i := range stops {
		stops[i] = tree.GradientStop{At: float64(i) / 6, Color: tree.HSVColor(float64(i)*60, 1, 1)}
	}
	return &tree.Gradient{Kind: tree.LinearKind, Angle: 90, Stops: stops}
}
