package paint

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// paintBoxShadows draws a node's outer or inset shadow layers. The shape is the
// node's box with the radius its painter uses, so a pill button gets a pill
// shadow without the design repeating the radius.
//
// Outer shadows are CSS box-shadow: the quad is the shape grown by blur+spread,
// so the shader can rebuild the shape it softens. An inset layer keeps the box
// as its quad and carries the offset and spread separately, because the shape it
// paints into is the hole shrunk from the inside.
func (l *List) paintBoxShadows(b *tree.NodeBase, r tree.Rect, clip tree.Rect, inset bool) {
	layers := b.Shadow()
	if len(layers) == 0 {
		return
	}
	radius := float32(l.shapeRadius(b, r))
	for _, s := range layers {
		if s.Inset != inset {
			continue
		}
		color := shadowColor(s, l.Theme)
		blur := float32(maxf(s.Blur, 0))
		blurF := float64(blur)
		if inset {
			l.Ops = append(l.Ops, Op{
				Kind:    Shadow,
				Inset:   true,
				Rect:    rectArr(r),
				Radius:  [4]float32{radius, radius, radius, radius},
				Color:   color,
				Blur:    blur,
				OffsetX: float32(s.X),
				OffsetY: float32(s.Y),
				Spread:  float32(maxf(s.Spread, 0)),
				Clip:    clipArr(clip),
			})
			continue
		}
		pad := blurF + maxf(s.Spread, 0)
		quad := tree.Rect{X: r.X + s.X - pad, Y: r.Y + s.Y - pad, W: r.W + 2*pad, H: r.H + 2*pad}
		rad := radius + float32(maxf(s.Spread, 0)) + blur
		l.Ops = append(l.Ops, Op{
			Kind:   Shadow,
			Rect:   rectArr(quad),
			Radius: [4]float32{rad, rad, rad, rad},
			Color:  color,
			Blur:   blur,
			Clip:   clipArr(clip),
		})
	}
}

// shadowColor is the layer's colour, or the scheme's shadow at a readable
// strength when the design named none. CSS defaults a shadow to black; using
// the role keeps a light-on-dark theme from drawing a black smudge.
func shadowColor(s tree.ShadowLayer, theme *m3.Theme) uint32 {
	if s.Color != 0 {
		return s.Color
	}
	if theme == nil {
		return 0x00000080
	}
	return withAlpha(theme.Colors.Shadow, 0.55)
}

// shapeRadius is the corner radius the node paints with. `radius` wins; without
// one, a component keeps the M3 shape it draws, so a shadow on a Card is not a
// pill and a shadow on a Button is not a square. A plain node has no corners.
func (l *List) shapeRadius(b *tree.NodeBase, r tree.Rect) float64 {
	if rad := b.Radius(); rad > 0 {
		return rad
	}
	pill := minf(r.W, r.H) / 2
	switch b.Node().Kind() {
	case "Button", "IconButton", "FAB", "Chip", "Badge", "Switch", "Slider",
		"SearchBar", "Tabs", "Tab", "NavigationItem":
		return pill
	case "FABMenu":
		return m3.ShapeLarge
	case "Card":
		return m3.ShapeMedium
	case "Dialog", "BottomSheet", "SideSheet", "Menu", "DatePicker", "TimePicker", "ColorPicker", "Carousel":
		return m3.ShapeExtraLarge
	case "Snackbar", "Tooltip", "TextField", "TextArea":
		return m3.ShapeExtraSmall
	}
	return 0
}
