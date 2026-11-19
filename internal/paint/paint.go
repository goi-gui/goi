// Package paint turns a laid-out tree into a flat display list. It never
// touches the GPU, so its output can be compared in tests.
//
// Material 3 visuals live in m3.go: every component is drawn here, so there is
// one place that decides what a Button or a Slider looks like.
package paint

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

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
	// Backdrop is the scene painted so far, blurred and masked to the op's
	// rounded box: CSS backdrop-filter. The renderer reads it as a request, not
	// as a draw, because it needs the frame in a texture first.
	Backdrop
	// Wipe is a theme transition's blur band. Rect is the moving band and Blur
	// its peak radius; the renderer reads it as a request to blur the finished
	// frame there, so the content melts instead of switching.
	Wipe
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
	// CSSGradient fills a shape with a CSS linear or radial gradient, using the
	// op's Gradient field for its stops.
	CSSGradient
	// Loading is the M3 loading shape: a rounded star that turns while it
	// morphs to a four-lobed rounded square and back. StartAngle is the turn,
	// Depth the lobe depth (0 flat, 1 a deep star) and Square how far it has
	// morphed to the square.
	Loading
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

	BorderColor     uint32
	BorderThickness float32
	// BorderDash is the style a rounded border draws in: 0 solid, 1 dashed,
	// 2 dotted. A border on a gradient reads it too.
	BorderDash float32

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

	// Arc fields, in radians, 0 at 12 o'clock and growing clockwise. A Loading
	// shape reads StartAngle as its turn.
	StartAngle float32
	EndAngle   float32
	Thickness  float32

	// Depth is a Loading shape's lobe depth, 0 (round) to 1 (a deep star).
	Depth float32
	// Square is how far a Loading shape has morphed from its twelve-lobed star
	// to the four-lobed rounded square, 0..1. Squash narrows that square along
	// one axis, which is how the M3 shape becomes its pill.
	Square float32
	Squash float32

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

	// Gradient is a CSS gradient fill, read only by CSSGradient. Its stops are
	// in 0..1 with the same colour format as Color.
	Gradient *tree.Gradient

	// Rotate is the op's turn in radians, about PivotX/PivotY in window space.
	// A node's `rotate` composes into every op its subtree emits.
	Rotate         float32
	PivotX, PivotY float32
	// Scale is x, y and a flag: the third slot is 1 when the op is scaled about
	// the same pivot, so an op nobody scaled keeps the zero value as identity.
	Scale [3]float32

	// Bitmap fields. Src is the decoded image and UV is the crop inside it, in
	// 0..1, so a cover or contain fit only changes the quad and the UVs.
	Src *img.Data
	UV  [4]float32
	// Invert flips the bitmap's RGB, keeping alpha. A map uses it to turn a
	// light raster tile set into something a dark scheme can sit on.
	Invert bool

	// Text fields.
	Text     string
	FontSize float32
	Weight   float32
	// FontFamily names a family the app registered with ui.LoadFont, or "" for
	// the built-in Roboto.
	FontFamily string
	// Italic slants the face; decoration uses it too, because the underline
	// follows the same glyphs.
	Italic bool
	Icon   bool
	// FilledIcon draws the icon in the filled Material Symbols instance; it is
	// only read when Icon is set.
	FilledIcon bool
	Align      Align
	TextX      float32 // left edge of the alignment box
	TextY      float32 // baseline
	BoxW       float32 // alignment box width
}

// List is a display list. Reusing the same List across frames keeps the
// allocation count flat.
type List struct {
	Ops   []Op
	Theme *m3.Theme
	// Measure returns the advance width of a string. The UI sets it to the real
	// shaper; when nil a rough estimate is used, which keeps tests font-free.
	Measure func(s string, size float64, weight int) float64
	// MeasureFamily is Measure with a font family, for the wrapping of text a
	// design set `fontFamily` on. Nil measures every run with Measure.
	MeasureFamily func(s string, size float64, weight int, family string) float64
	// ShowFPS draws the frame rate in the top-right corner, above the tree. It is
	// a debug overlay; the app turns it on with App.ShowFPS.
	ShowFPS bool
	FPS     int
	// suppressElev drops a component's own M3 elevation shadow for the node
	// being painted, so an explicit `shadow` replaces it instead of stacking.
	suppressElev bool
	// window is the whole client area, which a popover panel draws in: it is an
	// overlay, so a plain ancestor's box does not cut it. A scrolling ancestor
	// still does, or a panel would stay on screen after its anchor scrolled out.
	window tree.Rect
	// scrollClip is the intersection of the scrolling ancestors' viewports and
	// the window: what a floating panel may draw in.
	scrollClip tree.Rect
	// family is the font family of the node being painted, resolved with the
	// same inheritance a design has. Every text op the node emits carries it.
	family string
	// filledIcons is the `filledIcon` of the component being painted, so its
	// glyphs, including the ones it draws itself, come from the filled face.
	filledIcons bool
	// rippleDrawn is set by rippleNode, so a component that painted its own
	// ripple does not also get the generic one.
	rippleDrawn bool
	// filledLeading and filledTrailing are the per-slot properties: an icon in
	// one slot can be filled while the other stays outlined.
	filledLeading  bool
	filledTrailing bool
	// wrap caches wrapped text runs across frames: a repaint re-wraps every
	// visible paragraph, and the answer only changes with the text, the column
	// and the style. Reset keeps it, because it is still valid.
	wrap map[wrapKey][]text.Line
	// rich caches RichText line breaks, keyed by the paragraph's span version
	// and the column it was wrapped in.
	rich map[richKey][][]text.InlineRun
	// Clock is how long the window has been drawing, which is what moves a wave
	// on a wavy progress bar. A test sets it to look at a frame of its choosing.
	Clock time.Duration
	// Scale is the device pixel ratio, so a curve can be sampled finely enough
	// for the pixels it lands on. 0 and 1 mean one device pixel per unit.
	Scale float32
	// Animating reports whether this list drew something that moves on its own,
	// so the frame loop knows to draw the next one.
	Animating bool
}

// Reset empties the list but keeps its capacity.
func (l *List) Reset() {
	l.Ops = l.Ops[:0]
	l.Animating = false
}

// Build fills l from tr. size is the client area in logical pixels.
func (l *List) Build(tr *tree.Tree, w, h float64) {
	l.BuildClip(tr, w, h, tree.Rect{W: w, H: h})
}

// BuildClip builds the display list with an initial clip, which is how a theme
// wipe paints the old and new schemes into their halves of the window.
func (l *List) BuildClip(tr *tree.Tree, w, h float64, clip tree.Rect) {
	if l.Theme == nil {
		l.Theme = m3.Light()
	}
	l.Reset()
	l.window = tree.Rect{W: w, H: h}
	l.scrollClip = clip
	l.walk(tr.Root.Base(), 0, 0, clip)
	// A fullscreen video covers the window, so it is painted last rather than
	// where its own box sits; the walk skipped it in place.
	if v := tree.FullscreenVideo(tr); v != nil {
		if m, ok := tree.MediaNode(v); ok {
			l.paintBox(m.Base(), clip, clip)
			l.paintMediaFrame(m, clip, clip)
		}
	}
	if l.ShowFPS {
		l.textIn(strconv.Itoa(l.FPS)+" fps", l.style("labelSmall"), l.Theme.Colors.OnSurfaceVariant,
			tree.Rect{X: 0, Y: 8, W: w - 12, H: 20}, AlignEnd, clip)
	}
}

// panelPlace returns the bounds and clip a popover panel draws with: the box
// moved inside the window, and the scrolling ancestors' viewport as its clip. A
// panel floats above the page instead of living inside the element that holds
// it, but it still belongs to the scroll it was written in: an anchor scrolled
// out of view takes its panel with it.
func (l *List) panelPlace(b *tree.NodeBase, r tree.Rect, k *tree.NodeBase, kb tree.Rect, clip tree.Rect) (tree.Rect, tree.Rect) {
	p, ok := b.Node().(*tree.Popover)
	if !ok || p.Panel == nil || k != p.Panel.Base() {
		return kb, clip
	}
	abs := tree.Rect{X: r.X + kb.X, Y: r.Y + kb.Y, W: kb.W, H: kb.H}
	// An anchor outside its scroller has scrolled away: the panel stays where
	// the content put it and the clip above hides it. Flipping it into the
	// window here would leave a sliver of a panel whose anchor is nowhere.
	if vis := intersect(r, l.scrollClip); vis.W <= 0 || vis.H <= 0 {
		return kb, l.scrollClip
	}
	dx, dy := p.PanelShift(r, abs, l.window.W, l.window.H)
	kb.X += dx
	kb.Y += dy
	return kb, l.scrollClip
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
	outerFamily := l.family
	l.family = b.FontFamily()
	if !empty {
		l.paintContent(b, r, clip)
	}
	inner := b.ScrollClip(r)
	childClip := clip
	if b.ClipsChildren() {
		childClip = intersect(clip, inner)
	}
	outerScroll := l.scrollClip
	if b.ScrollAxis() != tree.AxisNone && b.ClipsChildren() {
		l.scrollClip = intersect(l.scrollClip, inner)
	}
	fromClip := len(l.Ops)
	for _, k := range b.PaintOrder() {
		kb := k.Bounds()
		kx, ky := b.ChildScrollShift(k)
		kb.X += kx
		kb.Y += ky
		if m, ok := b.Node().(*tree.Map); ok {
			if p, ok := k.Node().(*tree.Point); ok {
				if ax, ay, ok := p.Anchor(m); ok {
					kb.X, kb.Y = ax-kb.W/2, ay-kb.H/2
				} else {
					continue
				}
			}
		}
		kc := b.ChildClip(k, r, childClip)
		kb, kc = l.panelPlace(b, r, k, kb, kc)
		l.walkOffset(k, x, y, kb, kc)
	}
	frame := b.ScrollViewport(r)
	l.paintScrollShadows(b, frame, clip)
	l.paintScrollbar(b, frame, clip)
	if _, ok := b.Node().(componentNode); ok {
		l.paintComponentBorder(b, r, clip, from)
	}
	l.roundClip(b, r, fromClip)
	l.applyRotate(from, b, r)
	l.applyScale(from, b, r)
	l.applyOpacity(from, b.Opacity())
	l.applyDisabled(from, b)
	l.family = outerFamily
	l.scrollClip = outerScroll
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

// applyRotate turns every op a subtree emitted around the node's own centre. A
// nested rotation composes: the inner turn stays, and its pivot moves with the
// outer one, which is what a rigid transform does.
func (l *List) applyRotate(from int, b *tree.NodeBase, r tree.Rect) {
	rot := b.Rotation()
	if rot.IsZero() || from >= len(l.Ops) {
		return
	}
	// A two-dimensional turn is all this renderer can show; an x or y axis is a
	// 3D rotation whose projection is a scale, which is a different feature.
	angle := float32(rot.Angle)
	px := float32(r.X + r.W/2)
	py := float32(r.Y + r.H/2)
	c, s := float32(math.Cos(float64(angle))), float32(math.Sin(float64(angle)))
	for i := from; i < len(l.Ops); i++ {
		op := &l.Ops[i]
		if op.Rotate != 0 || op.Scale[2] != 0 {
			// Move the inner pivot into the outer frame, then add the angles. A
			// scaled op counts as transformed even without a turn: its pivot is
			// its own centre, and the turn has to carry it.
			dx, dy := op.PivotX-px, op.PivotY-py
			op.PivotX = px + dx*c - dy*s
			op.PivotY = py + dx*s + dy*c
			op.Rotate += angle
			continue
		}
		op.Rotate = angle
		op.PivotX, op.PivotY = px, py
	}
}

// applyScale stretches every op a subtree emitted about the node's centre, the
// way CSS `scale` does. Like rotation it is a paint transform: layout keeps the
// box where it was, and the clip still cuts in window space.
//
// shortcut: a non-uniform scale over a rotated child multiplies the factors
// instead of composing a matrix, so it is exact on aligned axes and an
// approximation when the two meet; upgrade by carrying a matrix per op.
func (l *List) applyScale(from int, b *tree.NodeBase, r tree.Rect) {
	sc := b.Scaling()
	if sc.IsIdentity() || from >= len(l.Ops) {
		return
	}
	sx, sy := float32(sc.X), float32(sc.Y)
	px, py := float32(r.X+r.W/2), float32(r.Y+r.H/2)
	for i := from; i < len(l.Ops); i++ {
		op := &l.Ops[i]
		if op.Rotate != 0 || op.Scale[2] != 0 {
			// The inner pivot moves under this scale, and the factors multiply.
			dx, dy := op.PivotX-px, op.PivotY-py
			op.PivotX, op.PivotY = px+dx*sx, py+dy*sy
			if op.Scale[2] == 0 {
				op.Scale = [3]float32{1, 1, 1}
			}
			op.Scale[0] *= sx
			op.Scale[1] *= sy
			continue
		}
		op.Scale = [3]float32{sx, sy, 1}
		op.PivotX, op.PivotY = px, py
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
	outerFamily := l.family
	l.family = b.FontFamily()
	if !empty {
		l.paintContent(b, r, clip)
	}

	inner := b.ScrollClip(r)
	childClip := clip
	if b.ClipsChildren() {
		childClip = intersect(clip, inner)
	}
	outerScroll := l.scrollClip
	if b.ScrollAxis() != tree.AxisNone && b.ClipsChildren() {
		l.scrollClip = intersect(l.scrollClip, inner)
	}
	fromClip := len(l.Ops)
	for _, k := range b.PaintOrder() {
		kb := k.Bounds()
		kx, ky := b.ChildScrollShift(k)
		kb.X += kx
		kb.Y += ky
		if m, ok := b.Node().(*tree.Map); ok {
			if p, ok := k.Node().(*tree.Point); ok {
				if ax, ay, ok := p.Anchor(m); ok {
					kb.X, kb.Y = ax-kb.W/2, ay-kb.H/2
				} else {
					continue
				}
			}
		}
		kc := b.ChildClip(k, r, childClip)
		kb, kc = l.panelPlace(b, r, k, kb, kc)
		l.walkOffset(k, x, y, kb, kc)
	}
	frame := b.ScrollViewport(r)
	l.paintScrollShadows(b, frame, clip)
	l.paintScrollbar(b, frame, clip)
	if _, ok := b.Node().(componentNode); ok {
		l.paintComponentBorder(b, r, clip, from)
	}
	l.roundClip(b, r, fromClip)
	l.applyRotate(from, b, r)
	l.applyScale(from, b, r)
	l.applyOpacity(from, b.Opacity())
	l.applyDisabled(from, b)
	l.family = outerFamily
	l.scrollClip = outerScroll
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
	if blur := b.BackdropBlur(); blur > 0 {
		l.paintBackdrop(b, r, clip, blur)
	}
	l.rippleDrawn = false
	from := len(l.Ops)
	if c, ok := b.Node().(componentNode); ok {
		l.suppressElev = b.ShadowSet()
		outerFilled, outerLead, outerTrail := l.filledIcons, l.filledLeading, l.filledTrailing
		l.filledIcons = c.M3().FilledIcon.Get()
		l.filledLeading = c.M3().FilledLeadingIcon.Get()
		l.filledTrailing = c.M3().FilledTrailingIcon.Get()
		l.paintComponent(c.M3(), r, clip)
		l.filledIcons, l.filledLeading, l.filledTrailing = outerFilled, outerLead, outerTrail
		l.suppressElev = false
		l.paintComponentBox(b, r, clip, from)
	} else {
		l.paintBox(b, r, clip)
	}
	if !isText {
		// An inset shadow belongs on top of the fill and below the content, the
		// way CSS paints it inside the padding box.
		l.paintBoxShadows(b, r, clip, true)
	}
	if m, ok := tree.MediaNode(b.Node()); ok {
		l.paintMediaContent(m, r, clip)
	}
	if t, ok := b.Node().(*tree.Text); ok {
		l.paintText(t, r, clip)
	}
	if rt, ok := b.Node().(*tree.RichText); ok {
		l.paintRichText(rt, r, clip)
	}
	if im, ok := b.Node().(*tree.Image); ok {
		l.paintImage(im, r, clip)
	}
	if s, ok := b.Node().(*tree.SVG); ok {
		l.paintSVG(s, r, clip)
	}
	if g, ok := b.Node().(*tree.Geom); ok {
		l.paintGeom(g, r, clip)
	}
	if p, ok := b.Node().(*tree.Path); ok {
		l.paintPath(p, r, clip)
	}
	// `ripple: true` on an element whose painter has no ripple of its own. It is
	// drawn last, above the bitmap or glyphs the element paints, or an image
	// would cover its own press feedback.
	if b.RippleSet() && b.WantsRipple() && !l.rippleDrawn {
		l.rippleNode(b, r, l.rippleRadius(b, r, from), l.Theme.Colors.OnSurface, clip)
	}
}

// paintBackdrop asks for a blurred copy of everything painted so far, masked to
// the element's rounded box. The element's own background then paints over it,
// so `background: #00000044` reads as tinted glass. A rotated element keeps an
// axis-aligned blur, because the blur samples the frame before the rotation.
func (l *List) paintBackdrop(b *tree.NodeBase, r tree.Rect, clip tree.Rect, blur float64) {
	radii := b.Radii()
	l.Ops = append(l.Ops, Op{
		Kind:   Backdrop,
		Rect:   rectArr(r),
		Radius: [4]float32{float32(radii[0]), float32(radii[1]), float32(radii[2]), float32(radii[3])},
		Color:  0xFFFFFFFF,
		Blur:   float32(blur),
		Clip:   clipArr(clip),
	})
}

// paintComponentBox applies the common box properties to a component. A
// component paints its own M3 surface, so background, border and radius used to
// be ignored there; this replaces the surface it drew, or puts one behind a
// transparent control, and draws the design's border above the content.
func (l *List) paintComponentBox(b *tree.NodeBase, r tree.Rect, clip tree.Rect, from int) {
	color := b.Background()
	grad := b.Gradient("background")
	hasPaint := color != 0 || grad != nil
	radii := b.Radii()
	hasRadius := b.RadiusSet()
	be, cols := b.BorderEdges(), b.BorderColors()
	hasColour := cols[0] != 0 || cols[1] != 0 || cols[2] != 0 || cols[3] != 0
	// A thickness alone restyles the outline the component already draws, so
	// there is nothing to replace it with: keep it. An explicit 0 draws no
	// border, and clearing the component's own is the point.
	hasBorder := b.BorderSet() && (hasColour || maxEdge(be) <= 0)
	if !hasPaint && !hasRadius && !hasBorder {
		return
	}

	// The component's outer surface is the first fill that covers its whole
	// box. Sliders, switches and checkboxes paint only their own small parts, so
	// they get a box behind their content instead.
	surface := surfaceOp(l.Ops, from, r)
	if surface >= 0 {
		op := &l.Ops[surface]
		if hasRadius {
			op.Radius = radiusArr(radii, r)
		}
		if hasPaint {
			op.Color = color
			op.Gradient = grad
			op.Shape = RoundedRect
			if grad != nil {
				op.Shape = CSSGradient
			}
		}
		if hasBorder {
			// The design's border replaces the component's own outline, which is
			// sometimes a second op on the same box (an outlined field, a table)
			// rather than a property of its surface. Children paint later, so
			// only the component's own filled boxes are in this range.
			for i := from; i < len(l.Ops); i++ {
				if op := &l.Ops[i]; op.Kind == Fill && rectEqual(op.Rect, r) {
					op.BorderColor, op.BorderThickness = 0, 0
				}
			}
		}
	} else if hasPaint {
		op := Op{
			Kind: Fill, Shape: RoundedRect,
			Rect: rectArr(r), Radius: radiusArr(radii, r),
			Color: color, Gradient: grad, Clip: clipArr(clip),
		}
		if grad != nil {
			op.Shape = CSSGradient
		}
		l.Ops = append(l.Ops, Op{})
		copy(l.Ops[from+1:], l.Ops[from:])
		l.Ops[from] = op
	}
}

// paintComponentBorder draws the design's border above a component's content
// and children. The border belongs to the whole box, not to the surface the
// component painted, so it runs after the child walk.
func (l *List) paintComponentBorder(b *tree.NodeBase, r tree.Rect, clip tree.Rect, from int) {
	if !b.BorderSet() {
		return
	}
	be, cols := b.BorderEdges(), b.BorderColors()
	if maxEdge(be) <= 0 {
		return
	}
	radii := b.Radii()
	if !b.RadiusSet() {
		// A pill button or a rounded card keeps its own corner when the design
		// only asked for a border colour.
		if i := surfaceOp(l.Ops, from, r); i >= 0 {
			for k := range radii {
				radii[k] = float64(l.Ops[i].Radius[k])
			}
		}
	}
	if b.UniformBorder() {
		if cols[0] != 0 && be.Top > 0 {
			before := len(l.Ops)
			l.fillShape(r, 0, nil, radii, cols[0], be.Top, clip)
			if len(l.Ops) > before {
				l.Ops[len(l.Ops)-1].BorderDash = dashOf(b)
			}
		}
		return
	}
	l.paintSideBorders(b, r, radii, be, cols, clip)
}

// surfaceOp is the first full-box rounded fill a component painted.
func surfaceOp(ops []Op, from int, r tree.Rect) int {
	for i := from; i < len(ops); i++ {
		op := &ops[i]
		if op.Kind == Fill && (op.Shape == RoundedRect || op.Shape == CSSGradient) && rectEqual(op.Rect, r) {
			return i
		}
	}
	return -1
}

// rectEqual reports whether a painted op covers the node's box, with the half
// pixel an op snaps to the device grid.
func rectEqual(op [4]float32, r tree.Rect) bool {
	const eps = 1.0
	return math.Abs(float64(op[0]-float32(r.X))) <= eps &&
		math.Abs(float64(op[1]-float32(r.Y))) <= eps &&
		math.Abs(float64(op[2]-float32(r.W))) <= eps &&
		math.Abs(float64(op[3]-float32(r.H))) <= eps
}

// paintBox draws the common box of an element: its background (a solid colour
// or a gradient) and its border. A component paints its own surface, so
// paintComponentBox applies the same properties to the fill it emitted.
func (l *List) paintBox(b *tree.NodeBase, r tree.Rect, clip tree.Rect) {
	radii := b.Radii()
	be := b.BorderEdges()
	cols := b.BorderColors()
	// A border whose sides agree is one rounded op. Per-side colours weight no
	// more than the fallback, so this stays the fast path for ordinary boxes.
	uniform := b.UniformBorder()
	borderColor, borderThickness := cols[0], be.Top
	if !uniform {
		borderColor, borderThickness = 0, 0
	}
	g := b.Gradient("background")
	if g != nil || b.Background() != 0 || borderThickness > 0 {
		before := len(l.Ops)
		l.fillShape(r, b.Background(), g, radii, borderColor, borderThickness, clip)
		if borderThickness > 0 && len(l.Ops) > before {
			l.Ops[len(l.Ops)-1].BorderDash = dashOf(b)
		}
	}
	if uniform {
		return
	}
	l.paintSideBorders(b, r, radii, be, cols, clip)
}

// maxEdge is the thickest side of a border.
func maxEdge(e tree.Edges) float64 {
	return math.Max(math.Max(e.Top, e.Right), math.Max(e.Bottom, e.Left))
}

// dashOf maps a border style to the shader's pattern code: 0 solid, 1 dashed,
// 2 dotted.
func dashOf(b *tree.NodeBase) float32 {
	switch b.BorderType() {
	case "dashed":
		return 1
	case "dotted":
		return 2
	}
	return 0
}

// fillShape is fillRect and fillGradient with one radius per corner, which is
// what a design with radiusTop or radiusTopLeft asks for. A uniform radius
// still goes through the same op; only the array differs.
func (l *List) fillShape(r tree.Rect, color uint32, g *tree.Gradient, radii [4]float64, borderColor uint32, borderThickness float64, clip tree.Rect) {
	if color == 0 && g == nil && (borderColor == 0 || borderThickness <= 0) {
		return
	}
	shape := RoundedRect
	if g != nil {
		shape = CSSGradient
	}
	l.Ops = append(l.Ops, Op{
		Kind:            Fill,
		Shape:           shape,
		Gradient:        g,
		Rect:            rectArr(r),
		Radius:          radiusArr(radii, r),
		Color:           color,
		BorderColor:     borderColor,
		BorderThickness: float32(borderThickness),
		Clip:            clipArr(clip),
	})
}

// notchedOutline fills r and draws its border with the top edge broken between
// gapStart and gapEnd, which is where a floating label sits. The ring is drawn
// in bands so the break does not leave a hole in the bottom or the sides; the
// corner band keeps the rounded ends whole.
func (l *List) notchedOutline(r tree.Rect, color uint32, g *tree.Gradient, radii [4]float64,
	borderColor uint32, thickness, gapStart, gapEnd float64, clip tree.Rect) {
	l.fillShape(r, color, g, radii, 0, 0, clip)
	if gapEnd <= gapStart || borderColor == 0 || thickness <= 0 {
		l.fillShape(r, 0, nil, radii, borderColor, thickness, clip)
		return
	}
	band := maxf(maxf(radii[0], radii[1]), maxf(radii[2], radii[3]))
	band = maxf(band, thickness)
	l.fillShape(r, 0, nil, radii, borderColor, thickness,
		intersect(clip, tree.Rect{X: r.X, Y: r.Y + band, W: r.W, H: r.H - band}))
	l.fillShape(r, 0, nil, radii, borderColor, thickness,
		intersect(clip, tree.Rect{X: r.X, Y: r.Y, W: gapStart - r.X, H: band}))
	l.fillShape(r, 0, nil, radii, borderColor, thickness,
		intersect(clip, tree.Rect{X: gapEnd, Y: r.Y, W: r.Right() - gapEnd, H: band}))
}

// paintSideBorders draws an uneven border as four bands, each in its own colour
// and width. The bands are cut to the box's rounded outline when every corner
// shares a radius; an uneven border on uneven corners keeps square corners,
// which is the one thing this cannot round.
//
// shortcut: per-side borders do not follow per-corner radii, upgrade by
// carrying four border widths into the shape shader.
func (l *List) paintSideBorders(b *tree.NodeBase, r tree.Rect, radii [4]float64, be tree.Edges, cols [4]uint32, clip tree.Rect) {
	inner := intersect(clip, r)
	rad := float32(0)
	if radii[0] == radii[1] && radii[1] == radii[2] && radii[2] == radii[3] {
		rad = float32(radii[0])
	}
	box := rectArr(r)
	side := func(band tree.Rect, color uint32) {
		if color == 0 || band.W <= 0 || band.H <= 0 {
			return
		}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect, Rect: rectArr(band), Color: color,
			Clip: clipArr(inner), ClipRadius: rad, ClipBox: box,
		})
	}
	side(tree.Rect{X: r.X, Y: r.Y, W: r.W, H: be.Top}, cols[0])
	side(tree.Rect{X: r.X, Y: r.Y, W: be.Left, H: r.H}, cols[3])
	side(tree.Rect{X: r.Right() - be.Right, Y: r.Y, W: be.Right, H: r.H}, cols[1])
	side(tree.Rect{X: r.X, Y: r.Bottom() - be.Bottom, W: r.W, H: be.Bottom}, cols[2])
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
	switch b.ScrollAxis() {
	case tree.AxisBoth:
		l.paintScrollbarAxis(b, frame, clip, tree.AxisX)
		l.paintScrollbarAxis(b, frame, clip, tree.AxisY)
	case tree.AxisNone:
	default:
		l.paintScrollbarAxis(b, frame, clip, b.ScrollAxis())
	}
}

// paintScrollbarAxis draws one thumb of a node that may scroll both ways.
func (l *List) paintScrollbarAxis(b *tree.NodeBase, frame tree.Rect, clip tree.Rect, axis tree.Axis) {
	thumb, ok := b.ScrollbarThumbFor(frame, axis)
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
	if !b.ScrollShadow() {
		return
	}
	switch b.ScrollAxis() {
	case tree.AxisBoth:
		l.paintScrollShadowsAxis(b, frame, clip, tree.AxisX)
		l.paintScrollShadowsAxis(b, frame, clip, tree.AxisY)
	case tree.AxisNone:
	default:
		l.paintScrollShadowsAxis(b, frame, clip, b.ScrollAxis())
	}
}

// paintScrollShadowsAxis draws the two edge fades of one axis.
func (l *List) paintScrollShadowsAxis(b *tree.NodeBase, frame tree.Rect, clip tree.Rect, axis tree.Axis) {
	off, max := b.ScrollOffsetFor(axis), b.ScrollMaxFor(axis)
	if max <= 0 {
		return
	}
	col := b.ScrollShadowColor()
	if col == 0 {
		col = withAlpha(l.Theme.Colors.Shadow, 0.45)
	}
	switch axis {
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
		op.Color = scaleAlpha(op.Color, a)
		op.BorderColor = scaleAlpha(op.BorderColor, a)
		for j := range op.TextShadows {
			op.TextShadows[j].Color = scaleAlpha(op.TextShadows[j].Color, a)
		}
	}
}

// paintImage appends a bitmap op, cropping the source to the node's box with the
// requested fit. A missing or undecoded image keeps the node's background, so a
// bad path shows as a coloured placeholder rather than a hole.
func (l *List) paintImage(n *tree.Image, r tree.Rect, clip tree.Rect) {
	if vis := intersect(clip, r); vis.W <= 0 || vis.H <= 0 {
		return
	}
	if l.paintCrossfade(n.Data, n.Fit.Get(), &n.NodeBase, &n.Fade, r, clip) {
		return
	}
	if n.Data == nil && !n.Loading && n.LoadErr == nil && !n.NoFile && !n.NoRetry && n.Src.Get() != "" {
		// The bitmap is gone without a load in flight: the image loader may
		// have lost it to an eviction after its sync pass. Paint the frame and
		// ask for another, where syncImages retries the load.
		l.Animating = true
	}
	l.paintBitmap(n.Data, n.Fit.Get(), &n.NodeBase, n.Loading, n.Phase, r, clip)
}

// paintSVG draws a rasterized SVG through the same bitmap path as Image.
func (l *List) paintSVG(n *tree.SVG, r tree.Rect, clip tree.Rect) {
	if l.paintCrossfade(n.Data, n.Fit.Get(), &n.NodeBase, &n.Fade, r, clip) {
		return
	}
	l.paintBitmap(n.Data, n.Fit.Get(), &n.NodeBase, n.Loading, n.Phase, r, clip)
}

// paintCrossfade draws the outgoing and incoming bitmaps at complementary
// alphas while a src change fades. It reports whether it painted the frame; a
// finished fade is cleared here, so the caller draws the new bitmap normally.
func (l *List) paintCrossfade(cur *img.Data, fit string, base *tree.NodeBase, fade *tree.SrcFade, r, clip tree.Rect) bool {
	if fade == nil || !fade.Active || fade.Prev == nil || cur == nil {
		return false
	}
	p := fade.Progress()
	if p >= 1 {
		return false
	}
	from := len(l.Ops)
	l.paintBitmap(fade.Prev, fit, base, false, 0, r, clip)
	l.applyOpacity(from, 1-p)
	from = len(l.Ops)
	l.paintBitmap(cur, fit, base, false, 0, r, clip)
	l.applyOpacity(from, p)
	l.Animating = true
	return true
}

func (l *List) paintBitmap(d *img.Data, fit string, base *tree.NodeBase, loading bool, phase float64, r tree.Rect, clip tree.Rect) {
	vis := intersect(clip, r)
	if vis.W <= 0 || vis.H <= 0 {
		return
	}
	if d == nil || d.W <= 0 || d.H <= 0 || r.W <= 0 || r.H <= 0 {
		// A remote image still on the way gets a breathing wash, so the space
		// reads as loading instead of broken. A failed or empty one stays the
		// node's own background.
		if loading {
			col := l.Theme.Colors
			alpha := 0.06 + 0.10*phase
			l.fillRect(r, withAlpha(col.OnSurface, alpha), base.Radius(), 0, 0, vis)
		}
		l.imageBorder(base, r, clip)
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
	// The bitmap's own radius rounds its crop; the clip radius rounds the
	// visible box. They are the same number for an image, but a map tile has no
	// rounding of its own and must take the map's corner from the clip.
	r0 := float32(base.Radius())
	l.Ops = append(l.Ops, Op{
		Kind: Bitmap, Rect: rectArr(quad), Clip: clipArr(vis),
		Color:  0xFFFFFFFF,
		Radius: [4]float32{r0, r0, r0, r0}, ClipRadius: r0,
		// The rounded crop follows the image's own box. Rounding the visible
		// rect instead would put a corner on the edge a scroll cut, so an image
		// half out of view would look rounded in the middle.
		ClipBox: rectArr(r),
		Src:     d, UV: uv,
	})
	l.imageBorder(base, r, clip)
}

// imageBorder draws the design's border over a bitmap: the image crops itself
// to its radius, so the ring is a separate shape on top rather than a border on
// the bitmap op.
func (l *List) imageBorder(b *tree.NodeBase, r tree.Rect, clip tree.Rect) {
	if !b.BorderSet() {
		return
	}
	be, cols := b.BorderEdges(), b.BorderColors()
	if maxEdge(be) <= 0 || cols[0] == 0 {
		return
	}
	radii := b.Radii()
	if b.UniformBorder() {
		before := len(l.Ops)
		l.fillShape(r, 0, nil, radii, cols[0], be.Top, clip)
		if len(l.Ops) > before {
			l.Ops[len(l.Ops)-1].BorderDash = dashOf(b)
		}
		return
	}
	l.paintSideBorders(b, r, radii, be, cols, clip)
}

// componentNode is implemented by every Material 3 component (they all embed
// tree.Component). It keeps paint free of a 36-way type assertion.
type componentNode interface {
	tree.Node
	M3() *tree.Component
}

// scrollShift is how far a node's children are moved by its scroll offset.
func scrollShift(b *tree.NodeBase) (x, y float64) {
	return b.ScrollShift()
}

// fillRect appends a rounded rectangle with an optional border.
func (l *List) fillRect(r tree.Rect, color uint32, radius float64, borderColor uint32, borderThickness float64, clip tree.Rect) {
	if color == 0 && (borderColor == 0 || borderThickness <= 0) {
		return
	}
	rad := float32(clampRadius(radius, r))
	l.Ops = append(l.Ops, Op{
		Kind:            Fill,
		Shape:           RoundedRect,
		Rect:            rectArr(r),
		Radius:          [4]float32{rad, rad, rad, rad},
		Color:           color,
		BorderColor:     borderColor,
		BorderThickness: float32(borderThickness),
		Clip:            clipArr(clip),
	})
}

// radiusArr clamps a four-corner radius to the shape, the way CSS treats a
// radius larger than the box: a 999dp radius on a 12dp square is a circle. The
// distance field cannot answer for a radius that large, so the clamp belongs
// here as well as in the shader.
func radiusArr(radii [4]float64, r tree.Rect) [4]float32 {
	if r.W > r.H {
		r.W = r.H
	}
	return [4]float32{
		float32(clampRadius(radii[0], r)),
		float32(clampRadius(radii[1], r)),
		float32(clampRadius(radii[2], r)),
		float32(clampRadius(radii[3], r)),
	}
}

// clampRadius limits one corner radius to half the shorter side.
func clampRadius(radius float64, r tree.Rect) float64 {
	limit := math.Min(r.W, r.H) / 2
	if radius < 0 {
		return 0
	}
	if radius > limit {
		return limit
	}
	return radius
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

// deviceScale is the list's device pixel ratio, never below one.
func (l *List) deviceScale() float64 {
	if l.Scale > 1 {
		return float64(l.Scale)
	}
	return 1
}

// strokeOr is a stroke width with the component's own fallback.
func strokeOr(v, def float64) float64 {
	if v > 0 {
		return v
	}
	return def
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
				op.Color, op.BorderColor, op.BorderThickness)
			if op.Shape == Arc {
				fmt.Fprintf(&b, " arc=%g..%g t=%g", op.StartAngle, op.EndAngle, op.Thickness)
			}
		case Shadow:
			fmt.Fprintf(&b, "shadow %g,%g %gx%g r=%g blur=%g color=#%08x",
				op.Rect[0], op.Rect[1], op.Rect[2], op.Rect[3], op.Radius[0], op.Blur, op.Color)
		case Backdrop:
			fmt.Fprintf(&b, "backdrop %g,%g %gx%g r=%g blur=%g alpha=%g",
				op.Rect[0], op.Rect[1], op.Rect[2], op.Rect[3], op.Radius[0], op.Blur,
				float32(op.Color&0xFF)/255)
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
