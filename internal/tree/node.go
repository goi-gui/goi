package tree

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/goi-gui/goi/internal/csscolor"
	"github.com/goi-gui/goi/internal/syntax"
)

// Rect is a rectangle in logical pixels, relative to the parent's content box.
type Rect struct {
	X, Y, W, H float64
}

// Right returns X+W.
func (r Rect) Right() float64 { return r.X + r.W }

// Bottom returns Y+H.
func (r Rect) Bottom() float64 { return r.Y + r.H }

// Contains reports whether x,y is inside the rect.
func (r Rect) Contains(x, y float64) bool {
	return x >= r.X && x < r.Right() && y >= r.Y && y < r.Bottom()
}

// Hooks is the app-facing side of one tree.
type Hooks struct {
	// Marked is called when a node becomes dirty, with the strongest kind.
	Marked func(DirtyKind)
	// Error receives a problem that only shows up while running, such as a
	// bridge handler that was never registered. The frame keeps going.
	Error func(error)
	// Media runs one playback method a design called on an Audio or Video by id.
	// The players live in the ui layer, so the tree forwards the request and
	// reports whether the node handled it.
	Media func(n Node, method string, args []any) bool
}

// NodeBase holds the state every node shares: identity, tree links, layout result
// and dirty flags.
type NodeBase struct {
	id     string
	parent *NodeBase
	kids   []*NodeBase

	bounds Rect

	// Scroll offsets are runtime state, clamped against the content extent when
	// read, so stale values after a relayout cannot scroll past the end.
	scrollX, scrollY float64

	// dirty is true when the node needs work; kind is the strongest of the
	// pending kinds (layout implies paint).
	dirty bool
	kind  DirtyKind

	visible Prop[bool]
	// floats marks a surface that draws outside its own box, and every ancestor
	// above it: paint and hit-testing order such a branch last, so a popover
	// panel stays above the siblings that follow it in the design.
	floats  bool
	margin  Prop[float64]
	padding Prop[float64]
	// Per-side overrides. A negative value follows margin/padding, so a
	// declaration can set one edge without repeating the others.
	marginTop, marginRight, marginBottom, marginLeft     Prop[float64]
	paddingTop, paddingRight, paddingBottom, paddingLeft Prop[float64]
	// Axis shorthands: left and right follow marginX/paddingX, top and bottom
	// follow marginY/paddingY, which follow margin/padding in turn.
	marginX, marginY                                     Prop[float64]
	paddingX, paddingY                                   Prop[float64]
	borderT                                              Prop[float64]
	borderTopT, borderRightT, borderBottomT, borderLeftT Prop[float64]
	// Axis shorthands for the border: left and right follow borderXThickness,
	// top and bottom borderYThickness, which follow borderThickness. The same
	// shape holds for borderXColor/borderYColor and borderColor.
	borderXT, borderYT         Prop[float64]
	borderXColor, borderYColor Prop[uint32]
	// borderType is solid, dashed or dotted, the way CSS spells it. Only a
	// drawn border reads it.
	borderType                         Prop[string]
	borderTopColor, borderRightColor   Prop[uint32]
	borderBottomColor, borderLeftColor Prop[uint32]
	flex                               Prop[float64]
	alignSelf                          Prop[string]
	// rotate turns the node and its subtree; layout ignores it, the way CSS
	// rotate does.
	rotate Prop[Rotation]
	// scale stretches the node and its subtree about its centre, the way CSS
	// `scale` does. Layout ignores it, so the box keeps its place.
	scale  Prop[Scale]
	scroll Prop[bool]
	// Scrollbar and scroll-shadow behaviour. `hideScrollbar` removes the bar;
	// `autoHideScrollbar` (the default) shows it only while the pointer is over
	// the area or the area scrolled recently.
	hideScrollbar     Prop[bool]
	autoHideScrollbar Prop[bool]
	scrollbarWidth    Prop[float64]
	scrollbarColor    Prop[uint32]
	scrollShadow      Prop[bool]
	scrollShadowColor Prop[uint32]
	// Scroll snapping, the way CSS spells it: the container carries the type,
	// the padding and the stop rule, and each child carries its alignment.
	scrollSnapType  Prop[string]
	scrollSnapStop  Prop[string]
	scrollSnapAlign Prop[string]
	scrollPadding   Prop[float64]
	// opacity fades the node and everything under it.
	opacity Prop[float64]
	// backdropBlur softens what is already painted behind this node, the way
	// CSS backdrop-filter does. It is one radius, or a gradient of radii; a
	// zero value paints nothing.
	backdropBlur Prop[BackdropBlur]
	// zIndex orders this node among its siblings when painting, like CSS: a
	// higher value paints later, so it lands on top. Layout ignores it.
	zIndex Prop[float64]
	// cursor is the pointer shape over this node, named the way CSS names it.
	// Empty means "auto": the framework's own affordance decides.
	cursor Prop[string]
	// scrollbarHover and scrollbarShowUntil are runtime state driven by input,
	// not properties: the bar's visibility is a fact about the pointer and the
	// clock, not something a design declares.
	scrollbarHover     bool
	scrollbarShowUntil time.Time
	// ripple is the design's word on press feedback; unset follows the host: a
	// component that paints a ripple keeps it, a plain element has none.
	ripple Prop[bool]
	// RippleOn, Ripple, RippleX and RippleY are the live press ripple: On marks
	// it running, 0..1 is how far it has expanded, and X/Y are its origin in
	// window coordinates. Input owns them; paint only reads them.
	RippleOn    bool
	Ripple      float64
	RippleX     float64
	RippleY     float64
	width       Prop[float64]
	height      Prop[float64]
	minWidth    Prop[float64]
	maxWidth    Prop[float64]
	minHeight   Prop[float64]
	maxHeight   Prop[float64]
	background  Prop[uint32]
	borderColor Prop[uint32]
	// gradient holds a CSS gradient written for a colour property, keyed by the
	// property name. Only `background` paints one today; the map keeps the
	// parser honest for the names that may grow later.
	gradient map[string]*csscolor.Gradient
	radius   Prop[float64]
	// Per-side and per-corner radii, all -1 unless a design sets them: `radius`
	// is the fallback, then the four sides, then the four corners.
	radiusTop, radiusRight, radiusBottom, radiusLeft Prop[float64]
	radiusTopLeft, radiusTopRight                    Prop[float64]
	radiusBottomRight, radiusBottomLeft              Prop[float64]
	// fontFamily names a family the app registered; empty follows the parent.
	fontFamily Prop[string]
	// shadow is the CSS-style shadow list. It paints outside the layout box, so
	// a change only repaints; Text draws it behind its glyphs and every other
	// node draws it around its box.
	shadow ShadowProp

	node   Node
	props  []NamedProp
	byName map[string]int
	hooks  *Hooks
	tree   *Tree
	AST    *syntax.Element // the element this node was built from
	// Click is the click handler slot. It lives on every node, so a plain
	// Rectangle can be a scrim or a hit area without becoming a component; a
	// component's own Click is the same field.
	Click Event[ClickEvent]
	// Outside is the click-outside handler slot, on every node for the same
	// reason: a press anywhere outside it is reported here.
	Outside Event[ClickEvent]
	// responsive holds properties declared as a breakpoint map.
	responsive []responsiveProp
	// conds holds properties whose value the first true conditional arm picks.
	conds []conditionalProp
	// exprs holds expression-valued properties, resolved every frame and (for
	// percentages) during layout.
	exprs      []exprSpec
	exprListed bool
	// colors holds properties written as theme roles, resolved whenever the
	// scheme changes.
	colors      []colorSpec
	colorListed bool
	// shadowListed marks a node whose shadow has a role colour, so the frame
	// loop resolves it with the rest of the theme. gradientListed does the same
	// for a background gradient whose stops name a colour.
	shadowListed   bool
	gradientListed bool
	// scope binds the {#each} loop variables an expression inside this subtree
	// may read.
	scope *scope
	// transitions maps a property to its duration; transitionStates holds the
	// live interpolation for a property that changed. transitionListed keeps the
	// node in the tree-level advance list.
	transitions      map[string]transitionDef
	transitionStates map[string]*transitionState
	transitionListed bool
}

// NodeBase returns the node's shared state.
func (b *NodeBase) Base() *NodeBase { return b }

// ID returns the id declared in the .goi file, or "".
func (b *NodeBase) ID() string { return b.id }

// Parent returns the parent node, or nil for the root.
func (b *NodeBase) Parent() Node {
	if b.parent == nil {
		return nil
	}
	return b.parent.node
}

// Children returns the child nodes.
func (b *NodeBase) Children() []Node {
	out := make([]Node, len(b.kids))
	for i, c := range b.kids {
		out[i] = c.node
	}
	return out
}

// TreeRef returns the tree this node was built into, or nil for a detached node.
func (b *NodeBase) TreeRef() *Tree { return b.tree }

// Node returns the concrete node behind this base.
func (b *NodeBase) Node() Node { return b.node }

// Kids returns the child nodes without copying. Internal to the framework:
// layout and paint walk it on the UI goroutine.
func (b *NodeBase) Kids() []*NodeBase { return b.kids }

// Bounds returns the rectangle assigned by the last layout pass, relative to
// the parent.
func (b *NodeBase) Bounds() Rect { return b.bounds }

// SetBounds is used by the layout engine.
func (b *NodeBase) SetBounds(r Rect) { b.bounds = r }

// Visible reports whether the node takes part in layout and hit-testing.
func (b *NodeBase) Visible() bool { return b.visible.Get() }

// SetVisible shows or hides the node. A hidden node and its children are
// skipped by layout, paint and hit-testing.
func (b *NodeBase) SetVisible(v bool) { b.visible.Set(v) }

// Edges is an inset per side, in logical pixels.
type Edges struct{ Top, Right, Bottom, Left float64 }

// Margin returns the outer spacing.
func (b *NodeBase) Margin() float64 { return b.margin.Get() }

// Padding returns the inner spacing.
func (b *NodeBase) Padding() float64 { return b.padding.Get() }

// MarginEdges returns the outer spacing per side: a side override wins, and an
// unset side follows margin.
func (b *NodeBase) MarginEdges() Edges {
	v := b.margin.Get()
	x, y := edgeOr(b.marginX.Get(), v), edgeOr(b.marginY.Get(), v)
	return Edges{
		Top:    edgeOr(b.marginTop.Get(), y),
		Right:  edgeOr(b.marginRight.Get(), x),
		Bottom: edgeOr(b.marginBottom.Get(), y),
		Left:   edgeOr(b.marginLeft.Get(), x),
	}
}

// PaddingEdges returns the inner spacing per side. A side wins over its axis
// (`paddingX`/`paddingY`), which wins over `padding`, so a design can set a
// vertical rhythm once and still override one edge.
func (b *NodeBase) PaddingEdges() Edges {
	v := b.padding.Get()
	x, y := edgeOr(b.paddingX.Get(), v), edgeOr(b.paddingY.Get(), v)
	return Edges{
		Top:    edgeOr(b.paddingTop.Get(), y),
		Right:  edgeOr(b.paddingRight.Get(), x),
		Bottom: edgeOr(b.paddingBottom.Get(), y),
		Left:   edgeOr(b.paddingLeft.Get(), x),
	}
}

// ContentBox returns r inset by the border and the padding, clamped to empty.
func (b *NodeBase) ContentBox(r Rect) Rect {
	pe, be := b.PaddingEdges(), b.BorderEdges()
	w := r.W - pe.Left - pe.Right - be.Left - be.Right
	h := r.H - pe.Top - pe.Bottom - be.Top - be.Bottom
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return Rect{X: r.X + pe.Left + be.Left, Y: r.Y + pe.Top + be.Top, W: w, H: h}
}

// ScrollClip is the box children clip to. A scrolling node clips to its frame:
// padding belongs to the moving content, so it scrolls away instead of leaving
// a fixed gutter at any edge. At rest the start padding is visible because the
// content is laid out after it, and the end padding appears once the area is
// scrolled all the way there. A node that does not scroll keeps the content box,
// because its padding is a permanent inset for what it draws.
func (b *NodeBase) ScrollClip(r Rect) Rect {
	if b.ScrollAxis() == AxisNone {
		return b.ContentBox(r)
	}
	return b.ScrollViewport(r)
}

// ScrollViewport is the box the scrollbar and scroll shadows attach to: the
// border box minus the border, ignoring padding. Padding spaces the moving
// content only, so the chrome stays against the frame.
func (b *NodeBase) ScrollViewport(r Rect) Rect {
	be := b.BorderEdges()
	w := r.W - be.Left - be.Right
	h := r.H - be.Top - be.Bottom
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return Rect{X: r.X + be.Left, Y: r.Y + be.Top, W: w, H: h}
}

func edgeOr(v, fallback float64) float64 {
	if v < 0 {
		return fallback
	}
	return v
}

// BorderThickness returns the border thickness in logical pixels, the value a
// single-width border reads. An uneven border asks BorderEdges per side.
func (b *NodeBase) BorderThickness() float64 { return b.borderT.Get() }

// BorderEdges returns the border thickness per side: a side override wins, and
// an unset side follows borderThickness.
func (b *NodeBase) BorderEdges() Edges {
	v := b.borderT.Get()
	if v < 0 {
		// -1 is "no border of its own": only the sides that name a thickness
		// draw, and a component keeps its own outline.
		v = 0
	}
	x, y := edgeOr(b.borderXT.Get(), v), edgeOr(b.borderYT.Get(), v)
	return Edges{
		Top:    edgeOr(b.borderTopT.Get(), y),
		Right:  edgeOr(b.borderRightT.Get(), x),
		Bottom: edgeOr(b.borderBottomT.Get(), y),
		Left:   edgeOr(b.borderLeftT.Get(), x),
	}
}

// BorderColors returns the four side colours in paint order: top, right,
// bottom, left. A side that was not written follows borderColor.
func (b *NodeBase) BorderColors() [4]uint32 {
	v := b.borderColor.Get()
	axis := func(c Prop[uint32]) uint32 {
		if got := c.Get(); got != 0 {
			return got
		}
		return v
	}
	x, y := axis(b.borderXColor), axis(b.borderYColor)
	pick := func(c Prop[uint32], fallback uint32) uint32 {
		if got := c.Get(); got != 0 {
			return got
		}
		return fallback
	}
	return [4]uint32{
		pick(b.borderTopColor, y), pick(b.borderRightColor, x),
		pick(b.borderBottomColor, y), pick(b.borderLeftColor, x),
	}
}

// BorderSet reports whether the design declared a border, uniformly or on a
// single side. Thickness defaults to -1, so an explicit 0 is a declaration too:
// it is what removes a component's own outline. A component whose box the
// design styled draws that border in place of its own.
func (b *NodeBase) BorderSet() bool {
	if b.borderT.Get() >= 0 || b.borderXT.Get() >= 0 || b.borderYT.Get() >= 0 {
		return true
	}
	return b.borderTopT.Get() >= 0 || b.borderRightT.Get() >= 0 ||
		b.borderBottomT.Get() >= 0 || b.borderLeftT.Get() >= 0
}

// BorderTypeValues lists the styles a border can draw in.
var BorderTypeValues = []string{"solid", "dashed", "dotted"}

// BorderType returns the style a border draws in: solid, dashed or dotted.
func (b *NodeBase) BorderType() string { return b.borderType.Get() }

// RadiusSet reports whether the design declared a corner radius. A component
// uses it for the same reason it uses BorderSet.
func (b *NodeBase) RadiusSet() bool { return b.Radius() > 0 }

// UniformBorder reports whether every side shares one colour and width, which
// is the case paint can draw with a single rounded op.
func (b *NodeBase) UniformBorder() bool {
	cols := b.BorderColors()
	be := b.BorderEdges()
	for i := 0; i < 4; i++ {
		if cols[i] != cols[0] {
			return false
		}
		switch i {
		case 1:
			if be.Right != be.Top {
				return false
			}
		case 2:
			if be.Bottom != be.Top {
				return false
			}
		case 3:
			if be.Left != be.Top {
				return false
			}
		}
	}
	return true
}

// Radii returns the four corner radii in paint order: top-left, top-right,
// bottom-right, bottom-left.
//
// Specificity runs from the general to the narrow one: `radius` sets all four,
// `radiusTop`/`radiusRight`/`radiusBottom`/`radiusLeft` set one side, and
// `radiusTopLeft` and friends set a single corner. A narrower property wins
// whatever the order in the file; of the two sides that meet at a corner, the
// horizontal one (left or right) wins over the vertical one.
func (b *NodeBase) Radii() [4]float64 {
	base := b.radius.Get()
	top, right := b.radiusTop.Get(), b.radiusRight.Get()
	bottom, left := b.radiusBottom.Get(), b.radiusLeft.Get()
	return [4]float64{
		edgeOr(b.radiusTopLeft.Get(), edgeOr(left, edgeOr(top, base))),
		edgeOr(b.radiusTopRight.Get(), edgeOr(right, edgeOr(top, base))),
		edgeOr(b.radiusBottomRight.Get(), edgeOr(right, edgeOr(bottom, base))),
		edgeOr(b.radiusBottomLeft.Get(), edgeOr(left, edgeOr(bottom, base))),
	}
}

// FontFamily returns the family the node draws text in: its own, else the
// nearest ancestor's, else "" for the built-in Roboto.
func (b *NodeBase) FontFamily() string {
	for n := b; n != nil; n = n.parent {
		if f := n.fontFamily.Get(); f != "" {
			return f
		}
	}
	return ""
}

// Flex returns the flex grow factor along the parent's main axis.
func (b *NodeBase) Flex() float64 { return b.flex.Get() }

// AlignSelf returns "" or start/center/end/stretch.
func (b *NodeBase) AlignSelf() string { return b.alignSelf.Get() }

// Rotation is a CSS `rotate` value in the form paint needs: an angle in radians
// about an axis. A two-dimensional rotation uses the z axis.
type Rotation struct {
	Angle float64 // radians
	Axis  [3]float64
}

// IsZero reports whether the rotation is a no-op.
func (r Rotation) IsZero() bool { return r.Angle == 0 }

// Rotation returns the node's rotation.
func (b *NodeBase) Rotation() Rotation { return b.rotate.Get() }

// Scale is a CSS `scale` value in the form paint needs: factors about the
// element's centre. Z is kept for the three-value form even though the renderer
// is flat.
type Scale struct {
	X, Y, Z float64
	// Set separates the zero value, which is the identity a node built without
	// a scale carries, from an explicit `scale: 0` that hides the element.
	Set bool
}

// IdentityScale is the scale that changes nothing.
var IdentityScale = Scale{X: 1, Y: 1, Z: 1, Set: true}

// IsIdentity reports whether the scale changes nothing.
func (s Scale) IsIdentity() bool { return !s.Set || (s.X == 1 && s.Y == 1) }

// Scaling returns the node's scale.
func (b *NodeBase) Scaling() Scale { return b.scale.Get() }

// SetScale stretches the node and its subtree by the factors about its centre,
// like CSS `scale`. It marks the tree dirty, so an animation needs no repaint
// call.
func (b *NodeBase) SetScale(x, y float64) {
	b.scale.Set(Scale{X: x, Y: y, Z: 1, Set: true})
}

// SetRotation turns the node and its subtree by deg degrees about axis (the z
// axis by default), like CSS `rotate`.
func (b *NodeBase) SetRotation(deg float64, axis ...[3]float64) {
	r := Rotation{Angle: deg * math.Pi / 180, Axis: [3]float64{0, 0, 1}}
	if len(axis) > 0 {
		r.Axis = axis[0]
	}
	b.rotate.Set(r)
	b.mark(DirtyPaint)
}

// Width returns the declared width, or -1 for auto.
func (b *NodeBase) Width() float64 { return b.width.Get() }

// SetWidth declares the node's width in dp; a negative value returns it to
// auto, the same as the property's default. Like every Prop.Set it marks the
// node for layout, so the frame that follows lays it out at the new size.
func (b *NodeBase) SetWidth(v float64) { b.width.Set(v) }

// Height returns the declared height, or -1 for auto.
func (b *NodeBase) Height() float64 { return b.height.Get() }

// SetHeight declares the node's height in dp; a negative value returns it to
// auto.
func (b *NodeBase) SetHeight(v float64) { b.height.Set(v) }

// SetSize declares both sides at once, the same as the design's `size`.
func (b *NodeBase) SetSize(w, h float64) {
	b.width.Set(w)
	b.height.Set(h)
}

// MinWidth returns the declared minimum width, or 0.
func (b *NodeBase) MinWidth() float64 { return b.minWidth.Get() }

// MaxWidth returns the declared maximum width, or -1 for unbounded.
func (b *NodeBase) MaxWidth() float64 { return b.maxWidth.Get() }

// MinHeight returns the declared minimum height, or 0.
func (b *NodeBase) MinHeight() float64 { return b.minHeight.Get() }

// MaxHeight returns the declared maximum height, or -1 for unbounded.
func (b *NodeBase) MaxHeight() float64 { return b.maxHeight.Get() }

// SetMinWidth changes the minimum width. A negative value clears it, the same
// as the property's default. Like every Prop.Set it marks the node for layout,
// and on Window the frame loop passes the new limit to the platform.
func (b *NodeBase) SetMinWidth(v float64) { b.minWidth.Set(v) }

// SetMaxWidth changes the maximum width; a negative value means unbounded.
func (b *NodeBase) SetMaxWidth(v float64) { b.maxWidth.Set(v) }

// SetMinHeight changes the minimum height; a negative value clears it.
func (b *NodeBase) SetMinHeight(v float64) { b.minHeight.Set(v) }

// SetMaxHeight changes the maximum height; a negative value means unbounded.
func (b *NodeBase) SetMaxHeight(v float64) { b.maxHeight.Set(v) }

// SetMargin changes the outer spacing; the per-side values the design set stay
// as they are and still win.
func (b *NodeBase) SetMargin(v float64) { b.margin.Set(v) }

// SetPadding changes the inner spacing; the per-side values the design set stay
// as they are and still win.
func (b *NodeBase) SetPadding(v float64) { b.padding.Set(v) }

// SetPaddingX sets the left and right padding; a negative value removes the
// override, so both follow padding again.
func (b *NodeBase) SetPaddingX(v float64) { b.paddingX.Set(v) }

// SetPaddingY sets the top and bottom padding; a negative value removes the
// override.
func (b *NodeBase) SetPaddingY(v float64) { b.paddingY.Set(v) }

// SetMarginTop, SetMarginRight, SetMarginBottom and SetMarginLeft override one
// side of the margin; a negative value removes the override, so that side
// follows margin again.
func (b *NodeBase) SetMarginX(v float64)      { b.marginX.Set(v) }
func (b *NodeBase) SetMarginY(v float64)      { b.marginY.Set(v) }
func (b *NodeBase) SetMarginTop(v float64)    { b.marginTop.Set(v) }
func (b *NodeBase) SetMarginRight(v float64)  { b.marginRight.Set(v) }
func (b *NodeBase) SetMarginBottom(v float64) { b.marginBottom.Set(v) }
func (b *NodeBase) SetMarginLeft(v float64)   { b.marginLeft.Set(v) }

// SetPaddingTop, SetPaddingRight, SetPaddingBottom and SetPaddingLeft override
// one side; a negative value removes the override, so the side follows its axis
// and then padding again.
func (b *NodeBase) SetPaddingTop(v float64)    { b.paddingTop.Set(v) }
func (b *NodeBase) SetPaddingRight(v float64)  { b.paddingRight.Set(v) }
func (b *NodeBase) SetPaddingBottom(v float64) { b.paddingBottom.Set(v) }
func (b *NodeBase) SetPaddingLeft(v float64)   { b.paddingLeft.Set(v) }

// SetFlex changes the node's share of its parent's leftover main-axis space.
func (b *NodeBase) SetFlex(v float64) { b.flex.Set(v) }

// SetAlignSelf overrides the parent's cross-axis alignment: "start", "center",
// "end", "stretch", or "" to inherit.
func (b *NodeBase) SetAlignSelf(v string) { b.alignSelf.Set(v) }

// SetScroll turns scrolling on or off for a node whose content overflows.
func (b *NodeBase) SetScroll(v bool) { b.scroll.Set(v) }

// SetRadius rounds all four corners.
func (b *NodeBase) SetRadius(v float64) { b.radius.Set(v) }

// SetRadiusTop, SetRadiusRight, SetRadiusBottom and SetRadiusLeft round both
// corners on one side; a negative value removes the override.
func (b *NodeBase) SetRadiusTop(v float64)    { b.radiusTop.Set(v) }
func (b *NodeBase) SetRadiusRight(v float64)  { b.radiusRight.Set(v) }
func (b *NodeBase) SetRadiusBottom(v float64) { b.radiusBottom.Set(v) }
func (b *NodeBase) SetRadiusLeft(v float64)   { b.radiusLeft.Set(v) }

// SetRadiusTopLeft, SetRadiusTopRight, SetRadiusBottomRight and
// SetRadiusBottomLeft round one corner; a negative value removes the override.
func (b *NodeBase) SetRadiusTopLeft(v float64)     { b.radiusTopLeft.Set(v) }
func (b *NodeBase) SetRadiusTopRight(v float64)    { b.radiusTopRight.Set(v) }
func (b *NodeBase) SetRadiusBottomRight(v float64) { b.radiusBottomRight.Set(v) }
func (b *NodeBase) SetRadiusBottomLeft(v float64)  { b.radiusBottomLeft.Set(v) }

// SetBorderTopThickness, SetBorderRightThickness, SetBorderBottomThickness and
// SetBorderLeftThickness draw one side thicker; a negative value removes the
// override, so the side follows borderThickness again.
func (b *NodeBase) SetBorderTopThickness(v float64)    { b.borderTopT.Set(v) }
func (b *NodeBase) SetBorderRightThickness(v float64)  { b.borderRightT.Set(v) }
func (b *NodeBase) SetBorderBottomThickness(v float64) { b.borderBottomT.Set(v) }
func (b *NodeBase) SetBorderLeftThickness(v float64)   { b.borderLeftT.Set(v) }

// SetBorderThickness draws an outline around the box; 0 removes it, and -1
// keeps a component's own outline.
func (b *NodeBase) SetBorderThickness(v float64) { b.borderT.Set(v) }

// SetBorderXThickness and SetBorderYThickness set the left and right, or the
// top and bottom, thickness at once; a negative value follows borderThickness
// again.
func (b *NodeBase) SetBorderXThickness(v float64) { b.borderXT.Set(v) }
func (b *NodeBase) SetBorderYThickness(v float64) { b.borderYT.Set(v) }

// SetBorderType picks the stroke: solid, dashed or dotted.
func (b *NodeBase) SetBorderType(v string) { b.borderType.Set(v) }

// WantsRipple reports the design's word on press feedback.
func (b *NodeBase) WantsRipple() bool { return b.ripple.Get() }

// RippleSet reports whether the design or Go wrote `ripple`, which is what tells
// an explicit false from the untouched default.
func (b *NodeBase) RippleSet() bool { return b.ripple.Written() }

// Background returns 0xRRGGBBAA.
func (b *NodeBase) Background() uint32 { return b.background.Get() }

// SetBackground paints a solid colour behind the node, replacing a role or a
// gradient the design set. Zero clears it.
func (b *NodeBase) SetBackground(rgba uint32) {
	b.clearColorRole("background")
	b.setGradient("background", nil)
	b.background.Set(rgba)
}

// Gradients returns the gradient behind a colour property, or nil for a solid
// one.
func (b *NodeBase) Gradient(name string) *csscolor.Gradient {
	if b.gradient == nil {
		return nil
	}
	return b.gradient[name]
}

// setGradient records or clears the gradient for a colour property.
func (b *NodeBase) setGradient(name string, g *csscolor.Gradient) {
	if g == nil {
		delete(b.gradient, name)
		return
	}
	if b.gradient == nil {
		b.gradient = map[string]*csscolor.Gradient{}
	}
	b.gradient[name] = g
	if g.HasRoles() {
		b.listGradient()
	}
}

// listGradient registers the node for gradient stop resolution.
func (b *NodeBase) listGradient() {
	if b.tree == nil || b.gradientListed {
		return
	}
	b.gradientListed = true
	b.tree.gradientNodes = append(b.tree.gradientNodes, b)
}

// resolveGradients rewrites the stops that name a colour, keeping the name so a
// later theme change resolves it again.
func (b *NodeBase) resolveGradients(lookup func(string) (uint32, bool)) bool {
	changed := false
	for _, g := range b.gradient {
		for i := range g.Stops {
			role := g.Stops[i].Role
			if role == "" {
				continue
			}
			rgba, ok := lookup(role)
			if !ok || g.Stops[i].Color == rgba {
				continue
			}
			g.Stops[i].Color = rgba
			changed = true
		}
	}
	return changed
}

// Opacity returns the node's opacity, 0 to 1. 1 is fully opaque.
func (b *NodeBase) Opacity() float64 {
	v := b.opacity.Get()
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// SetOpacity changes the opacity. Like every Prop.Set it marks the node dirty,
// so a tween can fade a subtree in or out.
func (b *NodeBase) SetOpacity(v float64) { b.opacity.Set(v) }

// BackdropBlur returns the peak radius, in logical pixels, of the blur applied
// to everything painted behind the node. 0 is no blur.
func (b *NodeBase) BackdropBlur() float64 { return b.backdropBlur.Get().Radius }

// BackdropBlurValue returns the radius or gradient the design wrote.
func (b *NodeBase) BackdropBlurValue() BackdropBlur { return b.backdropBlur.Get() }

// SetBackdropBlur sets a scalar radius.
func (b *NodeBase) SetBackdropBlur(v float64) { b.backdropBlur.Set(BackdropBlur{Radius: v}) }

// HideScrollbar reports whether the design removed the scrollbar.
func (b *NodeBase) HideScrollbar() bool { return b.hideScrollbar.Get() }

// AutoHideScrollbar reports whether the bar follows the pointer and the clock
// instead of always showing.
func (b *NodeBase) AutoHideScrollbar() bool { return b.autoHideScrollbar.Get() }

// SetHideScrollbar removes the bar, or brings it back.
func (b *NodeBase) SetHideScrollbar(v bool) { b.hideScrollbar.Set(v) }

// SetAutoHideScrollbar makes the bar follow the pointer and the clock, or keep
// it up.
func (b *NodeBase) SetAutoHideScrollbar(v bool) { b.autoHideScrollbar.Set(v) }

// SetScrollbarWidth changes the bar's thickness in dp.
func (b *NodeBase) SetScrollbarWidth(v float64) { b.scrollbarWidth.Set(v) }

// SetScrollbarColor paints the bar a solid colour, replacing a role the design
// set. Zero returns it to the theme's muted role.
func (b *NodeBase) SetScrollbarColor(rgba uint32) {
	b.clearColorRole("scrollbarColor")
	b.scrollbarColor.Set(rgba)
}

// SetScrollShadow turns the edge gradient on or off.
func (b *NodeBase) SetScrollShadow(v bool) { b.scrollShadow.Set(v) }

// SetScrollShadowColor paints the edge gradient a solid colour, replacing a
// role the design set.
func (b *NodeBase) SetScrollShadowColor(rgba uint32) {
	b.clearColorRole("scrollShadowColor")
	b.scrollShadowColor.Set(rgba)
}

// ScrollbarWidth returns the bar's thickness, falling back to the M3 default
// for a missing or unusable value.
func (b *NodeBase) ScrollbarWidth() float64 {
	v := b.scrollbarWidth.Get()
	if v <= 0 {
		return ScrollbarThickness
	}
	return v
}

// ScrollbarColor returns the bar colour, 0 when the design left it to the
// theme.
func (b *NodeBase) ScrollbarColor() uint32 { return b.scrollbarColor.Get() }

// ScrollShadow reports whether the area paints an edge gradient when its
// content is scrolled away from that edge.
func (b *NodeBase) ScrollShadow() bool { return b.scrollShadow.Get() }

// ScrollShadowColor returns the gradient colour, 0 when the design left it to
// the theme.
func (b *NodeBase) ScrollShadowColor() uint32 { return b.scrollShadowColor.Get() }

// SnapType returns the container's scroll-snap style: "", "none",
// "mandatory" or "proximity".
func (b *NodeBase) SnapType() string { return b.scrollSnapType.Get() }

// SetSnapType sets the container's scroll-snap style: "none", "mandatory" or
// "proximity".
func (b *NodeBase) SetSnapType(v string) { b.scrollSnapType.Set(v) }

// SetSnapStop ends a gesture at the next snap point when on.
func (b *NodeBase) SetSnapStop(v bool) {
	if v {
		b.scrollSnapStop.Set("stop")
		return
	}
	b.scrollSnapStop.Set("normal")
}

// SetSnapPadding insets the start and end snap lines from the frame.
func (b *NodeBase) SetSnapPadding(v float64) { b.scrollPadding.Set(v) }

// SnapAlign returns the child's snap alignment: "", "none", "start", "center"
// or "end".
func (b *NodeBase) SnapAlign() string { return b.scrollSnapAlign.Get() }

// SetSnapAlign sets where this child lines up with its scrolling parent:
// "none", "start", "center" or "end".
func (b *NodeBase) SetSnapAlign(v string) { b.scrollSnapAlign.Set(v) }

// SnapStop reports whether the container stops at the next snap point instead
// of letting a gesture pass more than one.
func (b *NodeBase) SnapStop() bool { return b.scrollSnapStop.Get() == "stop" }

// SnapPadding returns the inset between the frame and the snap line, the way
// CSS scroll-padding insets a start or end alignment.
func (b *NodeBase) SnapPadding() float64 { return b.scrollPadding.Get() }

// Snapping reports whether the node scrolls and asks for snapping at all.
func (b *NodeBase) Snapping() bool {
	t := b.SnapType()
	return b.ScrollAxis() != AxisNone && t != "" && t != "none"
}

// Cursor returns the pointer shape this node asks for, or "" for auto.
func (b *NodeBase) Cursor() string { return b.cursor.Get() }

// SetFontFamily sets the family the node and its subtree draw in, named the way
// the app registered it. "" hands the node back to its parent's family.
func (b *NodeBase) SetFontFamily(name string) { b.fontFamily.Set(name) }

// SetCursor changes the pointer shape over the node, using a CSS name.
func (b *NodeBase) SetCursor(name string) { b.cursor.Set(name) }

// ZIndex returns the node's paint order among its siblings.
func (b *NodeBase) ZIndex() float64 { return b.zIndex.Get() }

// SetZIndex restacks the node among its siblings. It marks the tree dirty, so
// raising a popover while it is open needs no repaint call.
func (b *NodeBase) SetZIndex(v float64) { b.zIndex.Set(v) }

// PaintOrder returns the children in paint order, by ascending zIndex, ties
// broken in favour of a branch that floats a surface out of its box, then by
// document order. A popover's panel is such a surface, so the branch that holds
// it paints above the siblings written after it — unless one of them names a
// zIndex, which is how a modal overlay stays on top of a page that contains a
// popover. Layout never uses this order, so a design can restack a row without
// moving anything. The untouching case costs nothing and is the common one.
func (b *NodeBase) PaintOrder() []*NodeBase {
	// A table paints its rows in the sorted order layout used, so a click lands
	// on the row under the pointer and the bands line up with the cells.
	if t, ok := b.Node().(*Table); ok {
		return t.paintOrder()
	}
	ordered := false
	for _, k := range b.kids {
		if k.zIndex.Get() != 0 || k.floats {
			ordered = true
			break
		}
	}
	if !ordered {
		return b.kids
	}
	out := make([]*NodeBase, len(b.kids))
	copy(out, b.kids)
	sort.SliceStable(out, func(i, j int) bool {
		if zi, zj := out[i].zIndex.Get(), out[j].zIndex.Get(); zi != zj {
			return zi < zj
		}
		if out[i].floats != out[j].floats {
			return !out[i].floats
		}
		return false
	})
	return out
}

// paintOrder is PaintOrder for a table: sorted rows, then the column headers
// and anything else the table holds.
func (t *Table) paintOrder() []*NodeBase {
	rows := t.TableRows()
	out := make([]*NodeBase, 0, len(t.Kids()))
	seen := make(map[*NodeBase]bool, len(rows))
	for _, r := range rows {
		out = append(out, &r.NodeBase)
		seen[&r.NodeBase] = true
	}
	for _, k := range t.Kids() {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}

// BorderColor returns 0xRRGGBBAA.
func (b *NodeBase) BorderColor() uint32 { return b.borderColor.Get() }

// SetBorderColor draws the outline in a solid colour, replacing a role the
// design set. Zero returns the property to the scheme's role.
func (b *NodeBase) SetBorderColor(rgba uint32) {
	b.clearColorRole("borderColor")
	b.borderColor.Set(rgba)
}

// SetBorderXColor and SetBorderYColor paint the left and right, or the top and
// bottom, in a solid colour. Zero falls back to borderColor.
func (b *NodeBase) SetBorderXColor(rgba uint32) {
	b.clearColorRole("borderXColor")
	b.borderXColor.Set(rgba)
}

// SetBorderYColor paints the top and bottom; zero falls back to borderColor.
func (b *NodeBase) SetBorderYColor(rgba uint32) {
	b.clearColorRole("borderYColor")
	b.borderYColor.Set(rgba)
}

// SetBorderTopColor, SetBorderRightColor, SetBorderBottomColor and
// SetBorderLeftColor paint one side in a solid colour, replacing a role the
// design set there. Zero falls back to its axis colour, then borderColor.
func (b *NodeBase) SetBorderTopColor(rgba uint32) {
	b.clearColorRole("borderTopColor")
	b.borderTopColor.Set(rgba)
}

// SetBorderRightColor paints the right side; zero falls back to borderColor.
func (b *NodeBase) SetBorderRightColor(rgba uint32) {
	b.clearColorRole("borderRightColor")
	b.borderRightColor.Set(rgba)
}

// SetBorderBottomColor paints the bottom side; zero falls back to borderColor.
func (b *NodeBase) SetBorderBottomColor(rgba uint32) {
	b.clearColorRole("borderBottomColor")
	b.borderBottomColor.Set(rgba)
}

// SetBorderLeftColor paints the left side; zero falls back to borderColor.
func (b *NodeBase) SetBorderLeftColor(rgba uint32) {
	b.clearColorRole("borderLeftColor")
	b.borderLeftColor.Set(rgba)
}

// Radius returns the largest corner radius, for the places that can only use
// one: clip rounding and the M3 shapes. A per-corner box asks Radii.
func (b *NodeBase) Radius() float64 {
	r := b.Radii()
	return max(max(r[0], r[1]), max(r[2], r[3]))
}

// Shadow returns the node's shadow layers, in paint order.
func (b *NodeBase) Shadow() []ShadowLayer { return b.shadow.layers }

// ShadowSet reports whether the design or Go set `shadow`, even to `none`. A
// component uses it to drop its own M3 elevation: an explicit shadow replaces
// that default rather than stacking on top of it.
func (b *NodeBase) ShadowSet() bool { return b.shadow.explicit }

// SetShadow replaces the node's shadows with the same layers a `.goi` file
// writes. A role colour is resolved per frame, so it still follows the theme.
//
// Call it from Bind: a hot reload rebuilds the nodes, and the design's own value
// wins again.
func (b *NodeBase) SetShadow(layers ...ShadowLayer) {
	if b.startGoShadow(layers) {
		b.shadow.goSet = true
		b.shadow.explicit = true
		return
	}
	b.shadow.set(layers)
	b.shadow.goSet = true
	b.shadow.explicit = true
	if b.shadow.hasRole() {
		b.listShadow()
	}
	b.mark(DirtyPaint)
}

// ClearShadow removes every shadow, including a component's default elevation.
func (b *NodeBase) ClearShadow() {
	if len(b.shadow.layers) == 0 && b.shadow.explicit {
		return
	}
	b.SetShadow()
}

// Dirty reports the pending work kind and whether any work is pending.
func (b *NodeBase) Dirty() (DirtyKind, bool) { return b.kind, b.dirty }

// MarkDirty forces a repaint, plus a relayout when kind is DirtyLayout.
func (b *NodeBase) MarkDirty(kind DirtyKind) { b.mark(kind) }

// ClearDirty is called by the frame loop after layout and paint ran.
func (b *NodeBase) ClearDirty() {
	b.dirty = false
	b.kind = DirtyPaint
}

func (b *NodeBase) mark(kind DirtyKind) {
	b.dirty = true
	if kind > b.kind {
		b.kind = kind
	}
	if kind == DirtyLayout {
		// A size change can move every ancestor, so layout dirt propagates up.
		for p := b.parent; p != nil; p = p.parent {
			p.dirty = true
			p.kind = DirtyLayout
		}
	}
	if b.hooks != nil && b.hooks.Marked != nil {
		b.hooks.Marked(kind)
	}
}

// properties returns the node's property table, built on first use.
func (b *NodeBase) properties() []NamedProp {
	if b.props == nil && b.node != nil {
		b.props = b.node.namedProps()
		b.byName = propIndexFor(b.node.Kind(), b.props)
	}
	return b.props
}

// propIndexes caches the name-to-position map for each element kind. A kind's
// property table is the same list in the same order for every node of that
// kind, so one map serves them all. Building one per node was the largest
// per-node allocation in a design: a 34-entry string map costs more than the
// node itself.
var propIndexes sync.Map // string -> map[string]int

func propIndexFor(kind string, props []NamedProp) map[string]int {
	if m, ok := propIndexes.Load(kind); ok {
		return m.(map[string]int)
	}
	m := make(map[string]int, len(props))
	for i := range props {
		m[props[i].Name] = i
	}
	propIndexes.Store(kind, m)
	return m
}

// PropValue returns the type-erased property value by .goi name.
func (b *NodeBase) PropValue(name string) (any, bool) {
	b.properties()
	if i, ok := b.byName[name]; ok {
		return b.props[i].Get(), true
	}
	return nil, false
}

// SetPropBool writes a bool property by .goi name through its cell, so a runtime
// write from a design method marks the tree dirty like any other Set. It
// reports false when the node has no such property.
func (b *NodeBase) SetPropBool(name string, v bool) bool {
	b.properties()
	i, ok := b.byName[name]
	if !ok {
		return false
	}
	p, ok := b.props[i].cell.(*Prop[bool])
	if !ok {
		return false
	}
	p.Set(v)
	return true
}

// PropDirty returns the dirty kind of a property, or DirtyLayout when unknown.
func (b *NodeBase) PropDirty(name string) DirtyKind {
	b.properties()
	if i, ok := b.byName[name]; ok {
		return b.props[i].Dirty
	}
	return DirtyLayout
}

// SetPropLiteral applies a parsed literal by .goi property name.
func (b *NodeBase) SetPropLiteral(name string, v syntax.Value) bool {
	b.properties()
	i, ok := b.byName[name]
	if !ok {
		return false
	}
	return b.props[i].SetLiteral(v)
}

// SetPropDefault applies a schema default without counting as a write, so a
// property that was never mentioned stays distinguishable from one set to its
// default value.
func (b *NodeBase) SetPropDefault(name string, v syntax.Value) bool {
	b.properties()
	i, ok := b.byName[name]
	if !ok {
		return false
	}
	return b.props[i].SetDefault(v)
}
