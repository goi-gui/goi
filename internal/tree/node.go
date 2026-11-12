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
	// template marks a hidden row prototype for ui.Fill. It is a build marker,
	// so a copy made by Instantiate drops it and acts like an ordinary row.
	template Prop[bool]
	margin   Prop[float64]
	padding  Prop[float64]
	// Per-side overrides. A negative value follows margin/padding, so a
	// declaration can set one edge without repeating the others.
	marginTop, marginRight, marginBottom, marginLeft     Prop[float64]
	paddingTop, paddingRight, paddingBottom, paddingLeft Prop[float64]
	// Axis shorthands: left and right follow paddingX, top and bottom follow
	// paddingY, which follow padding in turn.
	paddingX, paddingY                                   Prop[float64]
	borderW                                              Prop[float64]
	borderTopW, borderRightW, borderBottomW, borderLeftW Prop[float64]
	borderTopColor, borderRightColor                     Prop[uint32]
	borderBottomColor, borderLeftColor                   Prop[uint32]
	flex                                                 Prop[float64]
	alignSelf                                            Prop[string]
	// rotate turns the node and its subtree; layout ignores it, the way CSS
	// rotate does.
	rotate Prop[Rotation]
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
	// opacity fades the node and everything under it.
	opacity Prop[float64]
	// backdropBlur softens what is already painted behind this node, the way
	// CSS backdrop-filter does. 0 paints nothing.
	backdropBlur Prop[float64]
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
	width              Prop[float64]
	height             Prop[float64]
	minWidth           Prop[float64]
	maxWidth           Prop[float64]
	minHeight          Prop[float64]
	maxHeight          Prop[float64]
	background         Prop[uint32]
	borderColor        Prop[uint32]
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
	// loop resolves it with the rest of the theme.
	shadowListed bool
	// scope binds the {#each} loop variables an expression inside this subtree
	// may read.
	scope *scope
}

// NodeBase returns the node's shared state.
func (b *NodeBase) Base() *NodeBase { return b }

// ID returns the id declared in the .goi file, or "".
func (b *NodeBase) ID() string { return b.id }

// Template reports whether this node is a row prototype: `template: true` in
// the design hides it and lets ui.Fill build copies of it.
func (b *NodeBase) Template() bool { return b.template.Get() }

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
	return Edges{
		Top:    edgeOr(b.marginTop.Get(), v),
		Right:  edgeOr(b.marginRight.Get(), v),
		Bottom: edgeOr(b.marginBottom.Get(), v),
		Left:   edgeOr(b.marginLeft.Get(), v),
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

// BorderWidth returns the border thickness in logical pixels, the value a
// single-width border reads. An uneven border asks BorderEdges per side.
func (b *NodeBase) BorderWidth() float64 { return b.borderW.Get() }

// BorderEdges returns the border thickness per side: a side override wins, and
// an unset side follows borderWidth.
func (b *NodeBase) BorderEdges() Edges {
	v := b.borderW.Get()
	return Edges{
		Top:    edgeOr(b.borderTopW.Get(), v),
		Right:  edgeOr(b.borderRightW.Get(), v),
		Bottom: edgeOr(b.borderBottomW.Get(), v),
		Left:   edgeOr(b.borderLeftW.Get(), v),
	}
}

// BorderColors returns the four side colours in paint order: top, right,
// bottom, left. A side that was not written follows borderColor.
func (b *NodeBase) BorderColors() [4]uint32 {
	v := b.borderColor.Get()
	pick := func(c Prop[uint32]) uint32 {
		if got := c.Get(); got != 0 {
			return got
		}
		return v
	}
	return [4]uint32{
		pick(b.borderTopColor), pick(b.borderRightColor),
		pick(b.borderBottomColor), pick(b.borderLeftColor),
	}
}

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

// Height returns the declared height, or -1 for auto.
func (b *NodeBase) Height() float64 { return b.height.Get() }

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

// Background returns 0xRRGGBBAA.
func (b *NodeBase) Background() uint32 { return b.background.Get() }

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

// BackdropBlur returns the radius, in logical pixels, of the blur applied to
// everything painted behind the node. 0 is no blur.
func (b *NodeBase) BackdropBlur() float64 { return b.backdropBlur.Get() }

// SetBackdropBlur sets that radius.
func (b *NodeBase) SetBackdropBlur(v float64) { b.backdropBlur.Set(v) }

// HideScrollbar reports whether the design removed the scrollbar.
func (b *NodeBase) HideScrollbar() bool { return b.hideScrollbar.Get() }

// AutoHideScrollbar reports whether the bar follows the pointer and the clock
// instead of always showing.
func (b *NodeBase) AutoHideScrollbar() bool { return b.autoHideScrollbar.Get() }

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

// PaintOrder returns the children in paint order: ascending zIndex, ties in
// document order. Layout never uses it, so a design can restack a row without
// moving anything. The untouching case costs nothing and is the common one.
func (b *NodeBase) PaintOrder() []*NodeBase {
	// A table paints its rows in the sorted order layout used, so a click lands
	// on the row under the pointer and the bands line up with the cells.
	if t, ok := b.Node().(*Table); ok {
		return t.paintOrder()
	}
	ordered := false
	for _, k := range b.kids {
		if k.zIndex.Get() != 0 {
			ordered = true
			break
		}
	}
	if !ordered {
		return b.kids
	}
	out := make([]*NodeBase, len(b.kids))
	copy(out, b.kids)
	sort.SliceStable(out, func(i, j int) bool { return out[i].zIndex.Get() < out[j].zIndex.Get() })
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
