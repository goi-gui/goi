package tree

import "github.com/ksckaan1/goui/internal/syntax"

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

	visible     Prop[bool]
	margin      Prop[float64]
	padding     Prop[float64]
	borderW     Prop[float64]
	flex        Prop[float64]
	alignSelf   Prop[string]
	scroll      Prop[bool]
	width       Prop[float64]
	height      Prop[float64]
	minWidth    Prop[float64]
	maxWidth    Prop[float64]
	background  Prop[uint32]
	borderColor Prop[uint32]
	radius      Prop[float64]

	node   Node
	props  []NamedProp
	byName map[string]int
	hooks  *Hooks
	tree   *Tree
	AST    *syntax.Element // the element this node was built from
	// responsive holds properties declared as a breakpoint map.
	responsive []responsiveProp
	// numerics holds expression-valued numeric properties, resolved before
	// layout and (for percentages) during it.
	numerics      []numericSpec
	numericListed bool
}

// NodeBase returns the node's shared state.
func (b *NodeBase) Base() *NodeBase { return b }

// ID returns the id declared in the .ui file, or "".
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

// Margin returns the outer spacing.
func (b *NodeBase) Margin() float64 { return b.margin.Get() }

// Padding returns the inner spacing.
func (b *NodeBase) Padding() float64 { return b.padding.Get() }

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

// Background returns 0xRRGGBBAA.
func (b *NodeBase) Background() uint32 { return b.background.Get() }

// BorderColor returns 0xRRGGBBAA.
func (b *NodeBase) BorderColor() uint32 { return b.borderColor.Get() }

// Radius returns the corner radius.
func (b *NodeBase) Radius() float64 { return b.radius.Get() }

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

// PropValue returns the type-erased property value by .ui name.
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

// SetPropLiteral applies a parsed literal by .ui property name.
func (b *NodeBase) SetPropLiteral(name string, v syntax.Value) bool {
	b.properties()
	i, ok := b.byName[name]
	if !ok {
		return false
	}
	return b.props[i].SetLiteral(v)
}
