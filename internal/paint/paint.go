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

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
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
	// Wave is a sine stroke used for `decorationStyle: wavy`. Thickness is the
	// stroke width; the box height sets the amplitude.
	Wave
	// Gradient fades a colour to transparent towards one edge, which is what a
	// scroll shadow is. StartAngle picks the opaque edge (see paint's grad*
	// constants).
	Gradient
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
	// Inset draws the shadow inside the shape instead of behind it, the way
	// CSS `box-shadow: … inset` does. OffsetX/OffsetY and Spread are only read
	// for an inset shadow.
	Inset   bool
	OffsetX float32
	OffsetY float32
	Spread  float32

	// TextShadows are drawn behind a text run's glyphs, one offset copy per
	// layer. They are ignored by every other kind.
	TextShadows []tree.ShadowLayer

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
	// ClipBox is the rounded shape's own box, as x, y, w, h. Clip is the
	// intersection of every clip above the op, which a scrolled card makes
	// smaller than the shape; rounding that would put corners at the viewport
	// edge. Zero means "round Clip itself".
	ClipBox [4]float32

	// Bitmap fields. Src is the decoded image and UV is the crop inside it, in
	// 0..1, so a cover or contain fit only changes the quad and the UVs.
	Src *img.Data
	UV  [4]float32

	// Text fields.
	Text     string
	FontSize float32
	Weight   float32
	// Italic slants the face; decoration uses it too, because the underline
	// follows the same glyphs.
	Italic bool
	Icon   bool
	Align  Align
	TextX  float32 // left edge of the alignment box
	TextY  float32 // baseline
	BoxW   float32 // alignment box width
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
	// suppressElev drops a component's own M3 elevation shadow for the node
	// being painted, so an explicit `shadow` replaces it instead of stacking.
	suppressElev bool
	// wrap caches wrapped text runs across frames: a repaint re-wraps every
	// visible paragraph, and the answer only changes with the text, the column
	// and the style. Reset keeps it, because it is still valid.
	wrap map[wrapKey][]text.Line
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

	// from is where this node's own ops begin, so an opacity can fade the node
	// together with everything under it.
	from := len(l.Ops)
	if !empty {
		l.paintContent(b, r, clip)
	}
	inner := b.ScrollClip(r)
	childClip := clip
	if b.ClipsChildren() {
		childClip = intersect(clip, inner)
	}
	ox, oy := scrollShift(b)
	fromClip := len(l.Ops)
	for _, k := range b.PaintOrder() {
		kb := k.Bounds()
		kb.X += ox
		kb.Y += oy
		l.walkOffset(k, x, y, kb, childClip)
	}
	frame := b.ScrollViewport(r)
	l.paintScrollShadows(b, frame, clip)
	l.paintScrollbar(b, frame, clip)
	l.roundClip(b, r, fromClip)
	l.applyOpacity(from, b.Opacity())
	l.applyDisabled(from, b)
}

// applyDisabled dims a table row's content when the row asked for it. A cell is
// a container, so there is no single colour to change; fading the ops it drew
// is what a disabled row means.
func (l *List) applyDisabled(from int, b *tree.NodeBase) {
	if b.Node().Kind() != "TableCell" {
		return
	}
	if cell, ok := b.Node().(*tree.TableCell); ok && cell.RowDisabled() {
		l.applyOpacity(from, m3.DisabledOpacity)
	}
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

	from := len(l.Ops)
	if !empty {
		l.paintContent(b, r, clip)
	}

	inner := b.ScrollClip(r)
	childClip := clip
	if b.ClipsChildren() {
		childClip = intersect(clip, inner)
	}
	ox, oy := scrollShift(b)
	fromClip := len(l.Ops)
	for _, k := range b.PaintOrder() {
		kb := k.Bounds()
		kb.X += ox
		kb.Y += oy
		l.walkOffset(k, x, y, kb, childClip)
	}
	frame := b.ScrollViewport(r)
	l.paintScrollShadows(b, frame, clip)
	l.paintScrollbar(b, frame, clip)
	l.roundClip(b, r, fromClip)
	l.applyOpacity(from, b.Opacity())
	l.applyDisabled(from, b)
}

// paintContent draws one node's own visuals: its shadow, its fill and its
// text, image or SVG. Children are the caller's job, because they need the
// clip and the scroll offset.
func (l *List) paintContent(b *tree.NodeBase, r tree.Rect, clip tree.Rect) {
	// Text shadows its glyphs, not its box, so it opts out of the box shadow.
	_, isText := b.Node().(*tree.Text)
	if !isText {
		l.paintBoxShadows(b, r, clip, false)
	}
	if c, ok := b.Node().(componentNode); ok {
		l.suppressElev = b.ShadowSet()
		l.paintComponent(c.M3(), r, clip)
		l.suppressElev = false
	} else if b.Background() != 0 || b.BorderWidth() > 0 {
		l.fillRect(r, b.Background(), b.Radius(), b.BorderColor(), b.BorderWidth(), clip)
	}
	if !isText {
		// An inset shadow belongs on top of the fill and below the content, the
		// way CSS paints it inside the padding box.
		l.paintBoxShadows(b, r, clip, true)
	}
	if t, ok := b.Node().(*tree.Text); ok {
		l.paintText(t, r, clip)
	}
	if im, ok := b.Node().(*tree.Image); ok {
		l.paintImage(im, r, clip)
	}
	if s, ok := b.Node().(*tree.SVG); ok {
		l.paintSVG(s, r, clip)
	}
}

// roundClip rounds the clip of every op a subtree produced, which is what a
// ClipRRect does: the clip rect was already narrowed to the box, so only the
// corner radius has to travel down. Ops the subtree did not emit are untouched,
// and a nested ClipRRect has already rounded its own range to a smaller radius.
func (l *List) roundClip(b *tree.NodeBase, r tree.Rect, from int) {
	if b.Node().Kind() != "ClipRRect" || from >= len(l.Ops) {
		return
	}
	rad := float32(b.Radius())
	if rad <= 0 {
		return
	}
	box := [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)}
	for i := from; i < len(l.Ops); i++ {
		l.Ops[i].ClipRadius = rad
		l.Ops[i].ClipBox = box
	}
}

// paintScrollbar draws the M3 thumb for a scrolling node: 4dp wide, rounded, 2dp
// in from the trailing edge, onSurfaceVariant at 40%. It is drawn last, above
// the content, and only when the content actually overflows and the design left
// it visible.
func (l *List) paintScrollbar(b *tree.NodeBase, frame tree.Rect, clip tree.Rect) {
	if !b.ScrollbarVisible() {
		return
	}
	thumb, ok := b.ScrollbarThumb(frame)
	if !ok {
		return
	}
	col := b.ScrollbarColor()
	if col == 0 {
		col = withAlpha(l.Theme.Colors.OnSurfaceVariant, 0.4)
	}
	l.fillRect(thumb, col, b.ScrollbarWidth()/2, 0, 0, clip)
}

// scrollShadowBand is how deep the edge gradient reaches, in dp.
const scrollShadowBand = 28.0

// paintScrollShadows paints a gradient at each edge the content has scrolled
// away from, the way CSS scroll shadows do: present at the top once the content
// moves down, at the bottom until it reaches the end, and gone at rest.
func (l *List) paintScrollShadows(b *tree.NodeBase, frame tree.Rect, clip tree.Rect) {
	if !b.ScrollShadow() || b.ScrollMax() <= 0 {
		return
	}
	col := b.ScrollShadowColor()
	if col == 0 {
		col = withAlpha(l.Theme.Colors.Shadow, 0.45)
	}
	off := b.ScrollOffset()
	max := b.ScrollMax()
	switch b.ScrollAxis() {
	case tree.AxisY:
		band := minf(scrollShadowBand, frame.H/2)
		if off > 0 {
			l.gradient(tree.Rect{X: frame.X, Y: frame.Y, W: frame.W, H: band}, col, gradDown, b.Radius(), clip)
		}
		if off < max {
			l.gradient(tree.Rect{X: frame.X, Y: frame.Bottom() - band, W: frame.W, H: band}, col, gradUp, b.Radius(), clip)
		}
	case tree.AxisX:
		band := minf(scrollShadowBand, frame.W/2)
		if off > 0 {
			l.gradient(tree.Rect{X: frame.X, Y: frame.Y, W: band, H: frame.H}, col, gradRight, b.Radius(), clip)
		}
		if off < max {
			l.gradient(tree.Rect{X: frame.Right() - band, Y: frame.Y, W: band, H: frame.H}, col, gradLeft, b.Radius(), clip)
		}
	}
}

// Gradient directions: the edge the colour is opaque on, fading to nothing.
const (
	gradDown  = 0 // opaque at the top, for content scrolled down from it
	gradUp    = 1
	gradRight = 2 // opaque at the left
	gradLeft  = 3
)

// gradient appends one fade-to-transparent fill. radius rounds the corners so a
// shadow on a card follows its shape; the rounded far corners are invisible
// because the gradient is already transparent there.
func (l *List) gradient(r tree.Rect, color uint32, dir float32, radius float64, clip tree.Rect) {
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Gradient, Rect: rectArr(r), Color: color,
		StartAngle: dir, Thickness: float32(radius), Clip: clipArr(clip),
	})
}

// applyOpacity multiplies the alpha of every op a subtree emitted. The renderer
// has no offscreen pass, so a group cannot be composited as one: an overlap
// inside the subtree shows the two children over each other, exactly as CSS
// would without `isolation`. For the common case (a card or a fading list) the
// difference is invisible.
func (l *List) applyOpacity(from int, a float64) {
	if a >= 1 || from >= len(l.Ops) {
		return
	}
	for i := from; i < len(l.Ops); i++ {
		op := &l.Ops[i]
		op.Color = withAlpha(op.Color, a)
		op.BorderColor = withAlpha(op.BorderColor, a)
		for j := range op.TextShadows {
			op.TextShadows[j].Color = withAlpha(op.TextShadows[j].Color, a)
		}
	}
}

// paintImage appends a bitmap op, cropping the source to the node's box with the
// requested fit. A missing or undecoded image keeps the node's background, so a
// bad path shows as a coloured placeholder rather than a hole.
func (l *List) paintImage(n *tree.Image, r tree.Rect, clip tree.Rect) {
	l.paintBitmap(n.Data, n.Fit.Get(), &n.NodeBase, n.Loading, n.Phase, r, clip)
}

// paintSVG draws a rasterized SVG through the same bitmap path as Image.
func (l *List) paintSVG(n *tree.SVG, r tree.Rect, clip tree.Rect) {
	l.paintBitmap(n.Data, n.Fit.Get(), &n.NodeBase, n.Loading, n.Phase, r, clip)
}

func (l *List) paintBitmap(d *img.Data, fit string, base *tree.NodeBase, loading bool, phase float64, r tree.Rect, clip tree.Rect) {
	if d == nil || d.W <= 0 || d.H <= 0 || r.W <= 0 || r.H <= 0 {
		// A remote image still on the way gets a breathing wash, so the space
		// reads as loading instead of broken. A failed or empty one stays the
		// node's own background.
		if loading {
			col := l.Theme.Colors
			alpha := 0.06 + 0.10*phase
			l.fillRect(r, withAlpha(col.OnSurface, alpha), base.Radius(), 0, 0,
				intersect(clip, r))
		}
		return
	}
	uv := [4]float32{0, 0, 1, 1}
	quad := r
	sw, sh := float64(d.W), float64(d.H)
	switch fit {
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
		ClipRadius: float32(base.Radius()),
		Src:        d, UV: uv,
	})
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
