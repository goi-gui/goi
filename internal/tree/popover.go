package tree

// PopoverGap is the space between the anchor and the panel.
const PopoverGap = 8.0

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
		EnumProp(&c.NodeBase, "align", DirtyLayout, &c.Align, AlignValues))
}

// PanelShift moves the panel's box so it stays inside a window of winW by winH:
// it flips above the anchor when it would run off the bottom, then slides in
// from the edges. Layout keeps the preferred box; paint and hit-testing ask for
// this shift, so the picture and the press agree. anchor and panel are in
// window coordinates.
func (p *Popover) PanelShift(anchor, panel Rect, winW, winH float64) (dx, dy float64) {
	if winW <= 0 || winH <= 0 {
		return 0, 0
	}
	if panel.Bottom() > winH && anchor.Y-PopoverGap-panel.H >= 0 {
		dy = anchor.Y - PopoverGap - panel.H - panel.Y
	}
	x, y := panel.X, panel.Y+dy
	y = clampRange(y, 0, winH-panel.H)
	x = clampRange(x, 0, winW-panel.W)
	return x - panel.X, y - panel.Y
}

// clampRange keeps v inside [lo, hi]; an empty range (the box is wider than the
// window) takes lo, so the first edge stays visible.
func clampRange(v, lo, hi float64) float64 {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
