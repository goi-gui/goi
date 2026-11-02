// Package paint turns a laid-out tree into a flat display list. It never
// touches the GPU, so its output can be compared in tests.
//
// Material 3 visuals live in m3.go: every component is drawn here, so there is
// one place that decides what a Button or a Slider looks like.
package paint

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ksckaan1/goi/internal/img"
	"github.com/ksckaan1/goi/internal/m3"
	"github.com/ksckaan1/goi/internal/tree"
)

// Kind identifies a display list operation.
type Kind uint8

// Operation kinds.
const (
	// Fill is a solid shape: rectangle, circle or arc.
	Fill Kind = iota
	// Shadow is a soft drop shadow behind a rounded rectangle.
	Shadow
	// Text is a run of glyphs.
	Text
	// Bitmap samples a decoded image.
	Bitmap
)

// Shape is the geometry a Fill draws.
type Shape uint8

// Shapes.
const (
	RoundedRect Shape = iota
	Circle
	Arc
)

// Align is the horizontal alignment of a text or icon run inside its box.
type Align uint8

// Alignments.
const (
	AlignStart Align = iota
	AlignCenter
	AlignEnd
)

// Op is one drawing operation in window coordinates (logical pixels).
type Op struct {
	Kind  Kind
	Shape Shape

	// Rect is x, y, w, h. For Circle and Arc the circle is inscribed in it.
	Rect [4]float32
	// Radius is the per-corner radius: top-left, top-right, bottom-right,
	// bottom-left. ShapeFull is resolved by the painter before it gets here.
	Radius [4]float32
	Color  uint32 // 0xRRGGBBAA

	BorderColor uint32
	BorderWidth float32

	// Blur is the shadow falloff in logical pixels.
	Blur float32

	// Arc fields, in radians, 0 at 12 o'clock and growing clockwise.
	StartAngle float32
	EndAngle   float32
	Thickness  float32

	// Clip is the intersection of every ancestor's content box, as x, y, w, h.
	// Ops outside it are still emitted; the renderer discards those fragments.
	Clip [4]float32
	// ClipRadius rounds the corners of Clip, so an op can stay inside a pill or
	// a rounded card. 0 means a plain rectangle.
	ClipRadius float32

	// Bitmap fields. Src is the decoded image and UV is the crop inside it, in
	// 0..1, so a cover or contain fit only changes the quad and the UVs.
	Src *img.Data
	UV  [4]float32

	// Text fields.
	Text     string
	FontSize float32
	Weight   float32
	Icon     bool
	Align    Align
	TextX    float32 // left edge of the alignment box
	TextY    float32 // baseline
	BoxW     float32 // alignment box width
}

// List is a display list. Reusing the same List across frames keeps the
// allocation count flat.
type List struct {
	Ops   []Op
	Theme *m3.Theme
	// Measure returns the advance width of a string. The UI sets it to the real
	// shaper; when nil a rough estimate is used, which keeps tests font-free.
	Measure func(s string, size float64, weight int) float64
	// ShowFPS draws the frame rate in the top-right corner, above the tree. It is
	// a debug overlay; the app turns it on with App.ShowFPS.
	ShowFPS bool
	FPS     int
}

// Reset empties the list but keeps its capacity.
func (l *List) Reset() { l.Ops = l.Ops[:0] }

// Build fills l from tr. size is the client area in logical pixels.
func (l *List) Build(tr *tree.Tree, w, h float64) {
	if l.Theme == nil {
		l.Theme = m3.Light()
	}
	l.Reset()
	clip := tree.Rect{W: w, H: h}
	l.walk(tr.Root.Base(), 0, 0, clip)
	if l.ShowFPS {
		l.textIn(strconv.Itoa(l.FPS)+" fps", l.style("labelSmall"), l.Theme.Colors.OnSurfaceVariant,
			tree.Rect{X: 0, Y: 8, W: w - 12, H: 20}, AlignEnd, clip)
	}
}

// walk emits the node and its children. x,y is the node's absolute origin.
func (l *List) walk(b *tree.NodeBase, x, y float64, clip tree.Rect) {
	if !b.Visible() {
		return
	}
	br := b.Bounds()
	x += br.X
	y += br.Y
	r := tree.Rect{X: x, Y: y, W: br.W, H: br.H}
	// A node laid out with no area has nothing to show: its fill would be
	// invisible anyway, and an icon or a label would otherwise land on the
	// parent's origin (a collapsed FAB menu, for one). Children still walk, so
	// a zero-size Stack can hold anchored content.
	empty := br.W <= 0 || br.H <= 0

	if !empty {
		if c, ok := b.Node().(componentNode); ok {
			l.paintComponent(c.M3(), r, clip)
		} else if b.Background() != 0 || b.BorderWidth() > 0 {
			l.fillRect(r, b.Background(), b.Radius(), b.BorderColor(), b.BorderWidth(), clip)
		}
	}

	if !empty {
		if t, ok := b.Node().(*tree.Text); ok {
			l.paintText(t, r, clip)
		}
		if im, ok := b.Node().(*tree.Image); ok {
			l.paintImage(im, r, clip)
		}
	}
	inner := b.ContentBox(r)
	childClip := clip
	if b.ClipsChildren() {
		childClip = intersect(clip, inner)
	}
	ox, oy := scrollShift(b)
	from := len(l.Ops)
	for _, k := range b.Kids() {
		kb := k.Bounds()
		kb.X += ox
		kb.Y += oy
		l.walkOffset(k, x, y, kb, childClip)
	}
	l.paintScrollbar(b, r, inner, clip)
	l.roundClip(b, from)
}

// walkOffset is walk with an explicit bounds override, used for scrolled lists.
func (l *List) walkOffset(b *tree.NodeBase, x, y float64, bounds tree.Rect, clip tree.Rect) {
	if !b.Visible() {
		return
	}
	x += bounds.X
	y += bounds.Y
	r := tree.Rect{X: x, Y: y, W: bounds.W, H: bounds.H}
	empty := bounds.W <= 0 || bounds.H <= 0

	if !empty {
		if c, ok := b.Node().(componentNode); ok {
			l.paintComponent(c.M3(), r, clip)
		} else if b.Background() != 0 || b.BorderWidth() > 0 {
			l.fillRect(r, b.Background(), b.Radius(), b.BorderColor(), b.BorderWidth(), clip)
		}
	}
	if !empty {
		if t, ok := b.Node().(*tree.Text); ok {
			l.paintText(t, r, clip)
		}
		if im, ok := b.Node().(*tree.Image); ok {
			l.paintImage(im, r, clip)
		}
	}

	inner := b.ContentBox(r)
	childClip := clip
	if b.ClipsChildren() {
		childClip = intersect(clip, inner)
	}
	ox, oy := scrollShift(b)
	from := len(l.Ops)
	for _, k := range b.Kids() {
		kb := k.Bounds()
		kb.X += ox
		kb.Y += oy
		l.walkOffset(k, x, y, kb, childClip)
	}
	l.paintScrollbar(b, r, inner, clip)
	l.roundClip(b, from)
}

// roundClip rounds the clip of every op a subtree produced, which is what a
// ClipRRect does: the clip rect was already narrowed to the box, so only the
// corner radius has to travel down. Ops the subtree did not emit are untouched,
// and a nested ClipRRect has already rounded its own range to a smaller radius.
func (l *List) roundClip(b *tree.NodeBase, from int) {
	if b.Node().Kind() != "ClipRRect" || from >= len(l.Ops) {
		return
	}
	rad := float32(b.Radius())
	if rad <= 0 {
		return
	}
	for i := from; i < len(l.Ops); i++ {
		l.Ops[i].ClipRadius = rad
	}
}

// paintScrollbar draws the M3 thumb for a scrolling node: 4dp wide, rounded, 2dp
// in from the trailing edge, onSurfaceVariant at 40%. It is drawn last, above
// the content, and only when the content actually overflows.
func (l *List) paintScrollbar(b *tree.NodeBase, r, inner tree.Rect, clip tree.Rect) {
	axis := b.ScrollAxis()
	if axis == tree.AxisNone {
		return
	}
	max := b.ScrollMax()
	if max <= 0 {
		return
	}
	const thickness, inset, minLen = 4.0, 2.0, 32.0
	col := withAlpha(l.Theme.Colors.OnSurfaceVariant, 0.4)
	off := b.ScrollOffset()
	switch axis {
	case tree.AxisY:
		view := inner.H
		if view <= 0 {
			return
		}
		thumb := view * view / (view + max)
		if thumb < minLen {
			thumb = minLen
		}
		if thumb > view {
			thumb = view
		}
		l.fillRect(tree.Rect{
			X: inner.Right() - thickness - inset,
			Y: inner.Y + (view-thumb)*(off/max),
			W: thickness, H: thumb,
		}, col, thickness/2, 0, 0, clip)
	case tree.AxisX:
		view := inner.W
		if view <= 0 {
			return
		}
		thumb := view * view / (view + max)
		if thumb < minLen {
			thumb = minLen
		}
		if thumb > view {
			thumb = view
		}
		l.fillRect(tree.Rect{
			X: inner.X + (view-thumb)*(off/max),
			Y: inner.Bottom() - thickness - inset,
			W: thumb, H: thickness,
		}, col, thickness/2, 0, 0, clip)
	}
}

// paintImage appends a bitmap op, cropping the source to the node's box with the
// requested fit. A missing or undecoded image keeps the node's background, so a
// bad path shows as a coloured placeholder rather than a hole.
func (l *List) paintImage(n *tree.Image, r tree.Rect, clip tree.Rect) {
	d := n.Data
	if d == nil || d.W <= 0 || d.H <= 0 || r.W <= 0 || r.H <= 0 {
		// A remote image still on the way gets a breathing wash, so the space
		// reads as loading instead of broken. A failed or empty one stays the
		// node's own background.
		if n.Loading {
			l.paintImageLoading(n, r, clip)
		}
		return
	}
	uv := [4]float32{0, 0, 1, 1}
	quad := r
	sw, sh := float64(d.W), float64(d.H)
	switch n.Fit.Get() {
	case "contain":
		scale := r.W / sw
		if r.H/sh < scale {
			scale = r.H / sh
		}
		quad = tree.Rect{
			X: r.X + (r.W-sw*scale)/2,
			Y: r.Y + (r.H-sh*scale)/2,
			W: sw * scale, H: sh * scale,
		}
	case "fill":
		// The quad is the box; nothing is cropped.
	default: // cover
		scale := r.W / sw
		if scale < r.H/sh {
			scale = r.H / sh
		}
		visW, visH := r.W/scale, r.H/scale
		uv = [4]float32{
			float32((sw - visW) / 2 / sw), float32((sh - visH) / 2 / sh),
			float32((sw + visW) / 2 / sw), float32((sh + visH) / 2 / sh),
		}
	}
	// Clipping to the node's own box with its radius is what rounds the image;
	// the shader already knows how to round a clip.
	l.Ops = append(l.Ops, Op{
		Kind: Bitmap, Rect: rectArr(quad), Clip: clipArr(intersect(clip, r)),
		ClipRadius: float32(n.Base().Radius()),
		Src:        d, UV: uv,
	})
}

// paintImageLoading draws the placeholder for an image that is still being
// fetched: a faint wash over the box that breathes with Phase.
func (l *List) paintImageLoading(n *tree.Image, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	alpha := 0.06 + 0.10*n.Phase
	l.fillRect(r, withAlpha(col.OnSurface, alpha), n.Base().Radius(), 0, 0,
		intersect(clip, r))
}

// componentNode is implemented by every Material 3 component (they all embed
// tree.Component). It keeps paint free of a 36-way type assertion.
type componentNode interface {
	tree.Node
	M3() *tree.Component
}

// scrollShift is how far a node's children are moved by its scroll offset.
func scrollShift(b *tree.NodeBase) (x, y float64) {
	switch b.ScrollAxis() {
	case tree.AxisX:
		return -b.ScrollOffset(), 0
	case tree.AxisY:
		return 0, -b.ScrollOffset()
	}
	return 0, 0
}

// fillRect appends a rounded rectangle with an optional border.
func (l *List) fillRect(r tree.Rect, color uint32, radius float64, borderColor uint32, borderWidth float64, clip tree.Rect) {
	if color == 0 && (borderColor == 0 || borderWidth <= 0) {
		return
	}
	rad := float32(radius)
	l.Ops = append(l.Ops, Op{
		Kind:        Fill,
		Shape:       RoundedRect,
		Rect:        rectArr(r),
		Radius:      [4]float32{rad, rad, rad, rad},
		Color:       color,
		BorderColor: borderColor,
		BorderWidth: float32(borderWidth),
		Clip:        clipArr(clip),
	})
}

// clipArr and rectArr are tiny converters used everywhere below.
func rectArr(r tree.Rect) [4]float32 {
	return [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)}
}

func clipArr(r tree.Rect) [4]float32 {
	return [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)}
}

func intersect(a, b tree.Rect) tree.Rect {
	x0 := maxf(a.X, b.X)
	y0 := maxf(a.Y, b.Y)
	x1 := minf(a.Right(), b.Right())
	y1 := minf(a.Bottom(), b.Bottom())
	if x1 < x0 {
		x1 = x0
	}
	if y1 < y0 {
		y1 = y0
	}
	return tree.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Dump renders the list as text, for golden tests.
func (l *List) Dump() string {
	var b strings.Builder
	for _, op := range l.Ops {
		switch op.Kind {
		case Fill:
			name := "fill"
			if op.Shape == Circle {
				name = "circle"
			} else if op.Shape == Arc {
				name = "arc"
			}
			fmt.Fprintf(&b, "%s %g,%g %gx%g r=%g/%g/%g/%g color=#%08x border=#%08x/%g",
				name, op.Rect[0], op.Rect[1], op.Rect[2], op.Rect[3],
				op.Radius[0], op.Radius[1], op.Radius[2], op.Radius[3],
				op.Color, op.BorderColor, op.BorderWidth)
			if op.Shape == Arc {
				fmt.Fprintf(&b, " arc=%g..%g t=%g", op.StartAngle, op.EndAngle, op.Thickness)
			}
		case Shadow:
			fmt.Fprintf(&b, "shadow %g,%g %gx%g r=%g blur=%g color=#%08x",
				op.Rect[0], op.Rect[1], op.Rect[2], op.Rect[3], op.Radius[0], op.Blur, op.Color)
		case Text:
			kind := "text"
			if op.Icon {
				kind = "icon"
			}
			fmt.Fprintf(&b, "%s %q at %g,%g size=%g w=%g align=%d color=#%08x",
				kind, op.Text, op.TextX, op.TextY, op.FontSize, op.Weight, op.Align, op.Color)
		}
		fmt.Fprintf(&b, " clip=%g,%g %gx%g\n", op.Clip[0], op.Clip[1], op.Clip[2], op.Clip[3])
	}
	return b.String()
}
