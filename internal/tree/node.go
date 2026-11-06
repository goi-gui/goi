package tree

import "github.com/goi-gui/goi/internal/syntax"

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
	borderW                                              Prop[float64]
	flex                                                 Prop[float64]
	alignSelf                                            Prop[string]
	scroll                                               Prop[bool]
	width                                                Prop[float64]
	height                                               Prop[float64]
	minWidth                                             Prop[float64]
	maxWidth                                             Prop[float64]
	minHeight                                            Prop[float64]
	maxHeight                                            Prop[float64]
	background                                           Prop[uint32]
	borderColor                                          Prop[uint32]
	radius                                               Prop[float64]
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

// PaddingEdges returns the inner spacing per side, with the same fallback.
func (b *NodeBase) PaddingEdges() Edges {
	v := b.padding.Get()
	return Edges{
		Top:    edgeOr(b.paddingTop.Get(), v),
		Right:  edgeOr(b.paddingRight.Get(), v),
		Bottom: edgeOr(b.paddingBottom.Get(), v),
		Left:   edgeOr(b.paddingLeft.Get(), v),
	}
}

// ContentBox returns r inset by the border and the padding, clamped to empty.
func (b *NodeBase) ContentBox(r Rect) Rect {
	pe, bw := b.PaddingEdges(), b.BorderWidth()
	w := r.W - pe.Left - pe.Right - 2*bw
	h := r.H - pe.Top - pe.Bottom - 2*bw
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return Rect{X: r.X + pe.Left + bw, Y: r.Y + pe.Top + bw, W: w, H: h}
}

func edgeOr(v, fallback float64) float64 {
	if v < 0 {
		return fallback
	}
	return v
}

// BorderWidth returns the border thickness in logical pixels.
func (b *NodeBase) BorderWidth() float64 { return b.borderW.Get() }

// Flex returns the flex grow factor along the parent's main axis.
func (b *NodeBase) Flex() float64 { return b.flex.Get() }

// AlignSelf returns "" or start/center/end/stretch.
func (b *NodeBase) AlignSelf() string { return b.alignSelf.Get() }

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

// BorderColor returns 0xRRGGBBAA.
func (b *NodeBase) BorderColor() uint32 { return b.borderColor.Get() }

// Radius returns the corner radius.
func (b *NodeBase) Radius() float64 { return b.radius.Get() }

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
		b.byName = make(map[string]int, len(b.props))
		for i := range b.props {
			b.byName[b.props[i].Name] = i
		}
	}
	return b.props
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
