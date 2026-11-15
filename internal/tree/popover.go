package tree

// Popover floats a panel against the box of its children. Its children are the
// anchor, laid out as a row; the `$popover` slot holds the panel, which layout
// places under them and `align` lines up against their own box.
type Popover struct {
	Component
	// Align lines the panel up with the anchor: start, center or end.
	Align Prop[string]
	// Panel is the node built from the `$popover` slot, nil when the design
	// wrote none. It stays an ordinary child so paint and hit-testing reach it;
	// layout is the one place that treats it apart.
	Panel Node
}

// Kind implements Node.
func (*Popover) Kind() string { return "Popover" }

func (c *Popover) namedProps() []NamedProp {
	return append(c.propsExcept("Popover", "align"),
		EnumProp(&c.NodeBase, "align", DirtyLayout, &c.Align, "start", "center", "end"))
}
