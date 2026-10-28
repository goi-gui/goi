package tree

// Node is any element in the tree. The interface is intentionally narrow: all
// state lives in NodeBase, and namedProps is the only per-type hook.
type Node interface {
	Base() *NodeBase
	// Kind returns the .ui element name.
	Kind() string
	namedProps() []NamedProp
}

// commonProps returns the property table shared by every element. "id" is
// absent on purpose: it is assigned by the builder and is not a runtime cell.
func commonProps(b *NodeBase) []NamedProp {
	return []NamedProp{
		NumberProp(b, "width", DirtyLayout, &b.width),
		NumberProp(b, "height", DirtyLayout, &b.height),
		NumberProp(b, "minWidth", DirtyLayout, &b.minWidth),
		NumberProp(b, "maxWidth", DirtyLayout, &b.maxWidth),
		NumberProp(b, "margin", DirtyLayout, &b.margin),
		NumberProp(b, "padding", DirtyLayout, &b.padding),
		ColorProp(b, "background", DirtyPaint, &b.background),
		ColorProp(b, "borderColor", DirtyPaint, &b.borderColor),
		NumberProp(b, "borderWidth", DirtyLayout, &b.borderW),
		NumberProp(b, "radius", DirtyPaint, &b.radius),
		BoolProp(b, "visible", DirtyLayout, &b.visible),
		NumberProp(b, "flex", DirtyLayout, &b.flex),
		EnumProp(b, "alignSelf", DirtyLayout, &b.alignSelf, "start", "center", "end", "stretch"),
	}
}

// Window is the root of a tree. It owns the title but no size of its own; the
// platform window provides the outer size.
type Window struct {
	NodeBase
	Title Prop[string]
}

// Kind implements Node.
func (w *Window) Kind() string { return "Window" }

func (w *Window) namedProps() []NamedProp {
	return append(commonProps(&w.NodeBase), StringProp(&w.NodeBase, "title", &w.Title))
}

// Container holds the properties shared by Column, Row and Stack. It is never
// a node by itself; the three concrete types give each its own Go type so id
// lookups are type-checked.
type Container struct {
	NodeBase
	Spacing Prop[float64]
	Align   Prop[string]
}

func (c *Container) namedProps() []NamedProp {
	return append(commonProps(&c.NodeBase),
		NumberProp(&c.NodeBase, "spacing", DirtyLayout, &c.Spacing),
		EnumProp(&c.NodeBase, "align", DirtyLayout, &c.Align, "start", "center", "end", "stretch"),
	)
}

// Column stacks children vertically.
type Column struct{ Container }

// Kind implements Node.
func (*Column) Kind() string { return "Column" }

// Row places children horizontally.
type Row struct{ Container }

// Kind implements Node.
func (*Row) Kind() string { return "Row" }

// Stack overlays children, last on top.
type Stack struct{ Container }

// Kind implements Node.
func (*Stack) Kind() string { return "Stack" }

// Rectangle is a plain colored box.
type Rectangle struct {
	NodeBase
}

// Kind implements Node.
func (r *Rectangle) Kind() string { return "Rectangle" }

func (r *Rectangle) namedProps() []NamedProp { return commonProps(&r.NodeBase) }

// Text draws a single run of text.
type Text struct {
	NodeBase
	Text     Prop[string]
	FontSize Prop[float64]
	Color    Prop[uint32]
	Align    Prop[string]
	Wrap     Prop[bool]
}

// Kind implements Node.
func (t *Text) Kind() string { return "Text" }

func (t *Text) namedProps() []NamedProp {
	return append(commonProps(&t.NodeBase),
		StringProp(&t.NodeBase, "text", &t.Text),
		NumberProp(&t.NodeBase, "fontSize", DirtyLayout, &t.FontSize),
		ColorProp(&t.NodeBase, "color", DirtyPaint, &t.Color),
		EnumProp(&t.NodeBase, "align", DirtyLayout, &t.Align, "start", "center", "end"),
		BoolProp(&t.NodeBase, "wrap", DirtyLayout, &t.Wrap),
	)
}

// Button is a clickable label.
type Button struct {
	NodeBase
	Text            Prop[string]
	FontSize        Prop[float64]
	Color           Prop[uint32]
	HoverBackground Prop[uint32]

	Click Event[ClickEvent]

	hover bool
}

// Kind implements Node.
func (b *Button) Kind() string { return "Button" }

func (b *Button) namedProps() []NamedProp {
	return append(commonProps(&b.NodeBase),
		StringProp(&b.NodeBase, "text", &b.Text),
		NumberProp(&b.NodeBase, "fontSize", DirtyLayout, &b.FontSize),
		ColorProp(&b.NodeBase, "color", DirtyPaint, &b.Color),
		ColorProp(&b.NodeBase, "hoverBackground", DirtyPaint, &b.HoverBackground),
	)
}

// SetHover updates the hover state, repainting only on a change.
func (b *Button) SetHover(v bool) {
	if b.hover == v {
		return
	}
	b.hover = v
	b.MarkDirty(DirtyPaint)
}

// Hover reports whether the pointer is over the button.
func (b *Button) Hover() bool { return b.hover }

// TextInput is an editable single-line field.
type TextInput struct {
	NodeBase
	Text        Prop[string]
	Placeholder Prop[string]
	FontSize    Prop[float64]
	Color       Prop[uint32]

	Change Event[string]
	Submit Event[string]

	caret int
}

// Kind implements Node.
func (t *TextInput) Kind() string { return "TextInput" }

func (t *TextInput) namedProps() []NamedProp {
	return append(commonProps(&t.NodeBase),
		StringProp(&t.NodeBase, "text", &t.Text),
		StringProp(&t.NodeBase, "placeholder", &t.Placeholder),
		NumberProp(&t.NodeBase, "fontSize", DirtyLayout, &t.FontSize),
		ColorProp(&t.NodeBase, "color", DirtyPaint, &t.Color),
	)
}

// Caret returns the byte offset of the insertion point.
func (t *TextInput) Caret() int { return t.caret }

// SetCaret moves the insertion point, clamped to the text length.
func (t *TextInput) SetCaret(i int) {
	if i < 0 {
		i = 0
	}
	if n := len(t.Text.Get()); i > n {
		i = n
	}
	if t.caret != i {
		t.caret = i
		t.MarkDirty(DirtyPaint)
	}
}
