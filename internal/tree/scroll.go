package tree

import "time"

// Axis is a scroll direction.
type Axis uint8

// Scroll axes.
const (
	AxisNone Axis = iota
	AxisX
	AxisY
	// AxisBoth is a table's two-direction scroll: its columns can be wider than
	// the frame while its rows run past the bottom.
	AxisBoth
)

// ScrollAxis reports the direction a node scrolls, if any.
//
// A node scrolls when it sets `scroll: true`; a Row then scrolls horizontally
// and everything else vertically, so the axis follows the container's flow.
// A Table scrolls both ways: its columns keep their declared widths and the
// rows keep their heights, so either may overflow. The Material components that
// overflow their box scroll without the property, and a carousel scrolls
// sideways.
func (b *NodeBase) ScrollAxis() Axis {
	switch b.Node().Kind() {
	case "Carousel":
		return AxisX
	case "List", "Menu", "NavigationDrawer", "Dialog", "DatePicker":
		return AxisY
	}
	if !b.scroll.Get() {
		return AxisNone
	}
	switch b.Node().Kind() {
	case "Row", "TitleBar":
		return AxisX
	case "Table":
		return AxisBoth
	}
	return AxisY
}

// scrollsOn reports whether the node scrolls along one axis. AxisBoth scrolls
// on both.
func (b *NodeBase) scrollsOn(axis Axis) bool {
	a := b.ScrollAxis()
	return a == axis || a == AxisBoth
}

// ClipsChildren reports whether children paint clipped to this node's content
// box. Scrolling containers must clip their viewport, and components that draw
// a surface keep their content inside it. Layout containers do not clip, so a
// shadow or a focus ring is not cut at the edge of the row it sits in.
func (b *NodeBase) ClipsChildren() bool {
	if b.ScrollAxis() != AxisNone {
		return true
	}
	switch b.Node().Kind() {
	case "Column", "Row", "TitleBar", "Positioned", "Wrap", "Grid", "Toolbar", "ButtonGroup":
		return false
	}
	return true
}

// ScrollMax is the largest useful offset along the node's scroll axis: how far
// the children reach past the content box. A two-axis table reports its vertical
// extent; ask ScrollMaxFor for the other one.
func (b *NodeBase) ScrollMax() float64 {
	axis := b.ScrollAxis()
	if axis == AxisBoth {
		axis = AxisY
	}
	return b.ScrollMaxFor(axis)
}

// ScrollMaxFor is ScrollMax for one axis of a node that may scroll both ways.
func (b *NodeBase) ScrollMaxFor(axis Axis) float64 {
	if !b.scrollsOn(axis) {
		return 0
	}
	// Children are positioned from the outer box, so only the far edge inset
	// shrinks the visible area: content may run into the last padding.
	pe, be := b.PaddingEdges(), b.BorderEdges()
	view := b.bounds.H - pe.Bottom - be.Bottom
	if axis == AxisX {
		view = b.bounds.W - pe.Right - be.Right
	}
	end := 0.0
	for _, k := range b.kids {
		if !k.Visible() {
			continue
		}
		// A sticky header does not scroll, so it cannot widen the vertical
		// extent above the rows it sits over.
		if axis == AxisY && b.fixedChild(k) {
			continue
		}
		kb := k.Bounds()
		if axis == AxisX {
			if e := kb.X + kb.W; e > end {
				end = e
			}
			continue
		}
		if e := kb.Y + kb.H; e > end {
			end = e
		}
	}
	if m := end - view; m > 0 {
		return m
	}
	return 0
}

// ScrollOffset returns the clamped offset along the node's scroll axis.
func (b *NodeBase) ScrollOffset() float64 {
	axis := b.ScrollAxis()
	if axis == AxisBoth {
		axis = AxisY
	}
	return b.ScrollOffsetFor(axis)
}

// ScrollOffsetFor returns the clamped offset along one axis.
func (b *NodeBase) ScrollOffsetFor(axis Axis) float64 {
	var v float64
	switch axis {
	case AxisX:
		v = b.scrollX
	case AxisY:
		v = b.scrollY
	default:
		return 0
	}
	if v < 0 {
		return 0
	}
	if m := b.ScrollMaxFor(axis); v > m {
		return m
	}
	return v
}

// SetScrollOffset scrolls the node, storing the raw value: reads clamp, so the
// offset survives content shrinking and growing back.
func (b *NodeBase) SetScrollOffset(v float64) {
	axis := b.ScrollAxis()
	if axis == AxisBoth {
		axis = AxisY
	}
	b.SetScrollOffsetFor(axis, v)
}

// SetScrollOffsetFor scrolls one axis, storing the raw value like SetScrollOffset.
func (b *NodeBase) SetScrollOffsetFor(axis Axis, v float64) {
	switch axis {
	case AxisX:
		b.scrollX = v
	case AxisY:
		b.scrollY = v
	default:
		return
	}
	b.MarkDirty(DirtyPaint)
}

// fixedChild reports whether a child ignores the node's vertical scroll, which
// is what `stickyHeader` means on a table.
func (b *NodeBase) fixedChild(k *NodeBase) bool {
	t, ok := b.Node().(*Table)
	return ok && t.StickyHeader.Get() && k.Node().Kind() == "TableColumn"
}

// ScrollShift is how far a node's children are moved by its scroll offsets.
func (b *NodeBase) ScrollShift() (x, y float64) {
	if b.scrollsOn(AxisX) {
		x = -b.ScrollOffsetFor(AxisX)
	}
	if b.scrollsOn(AxisY) {
		y = -b.ScrollOffsetFor(AxisY)
	}
	return x, y
}

// ChildScrollShift is ScrollShift for one child, which is zero on the vertical
// axis of a sticky table header.
func (b *NodeBase) ChildScrollShift(k *NodeBase) (x, y float64) {
	x, y = b.ScrollShift()
	if y != 0 && b.fixedChild(k) {
		y = 0
	}
	return x, y
}

// Scrollbar metrics: a 4dp pill by default, 2dp in from the trailing edge,
// never shorter than 32dp so a long document still has something to grab. The
// thickness follows `scrollbarWidth`.
const (
	ScrollbarThickness = 4.0
	ScrollbarInset     = 2.0
	ScrollbarMinLength = 32.0
	// ScrollbarHideDelay is how long an auto-hiding bar stays after the pointer
	// leaves the area or the last scroll.
	ScrollbarHideDelay = 2 * time.Second
)

// ScrollbarThumb returns the thumb inside the viewport rect, and false when the
// node does not scroll or its content fits. Paint and hit-testing share it, so
// the bar a reader grabs is exactly the bar that was drawn. A two-axis table
// reports its vertical thumb; use ScrollbarThumbFor for the horizontal one.
func (b *NodeBase) ScrollbarThumb(inner Rect) (Rect, bool) {
	axis := b.ScrollAxis()
	if axis == AxisBoth {
		axis = AxisY
	}
	return b.ScrollbarThumbFor(inner, axis)
}

// ScrollbarThumbFor is ScrollbarThumb for one axis of a node.
func (b *NodeBase) ScrollbarThumbFor(inner Rect, axis Axis) (Rect, bool) {
	if !b.scrollsOn(axis) {
		return Rect{}, false
	}
	max := b.ScrollMaxFor(axis)
	if max <= 0 {
		return Rect{}, false
	}
	off := b.ScrollOffsetFor(axis)
	w := b.ScrollbarWidth()
	switch axis {
	case AxisY:
		view := inner.H
		if view <= 0 {
			return Rect{}, false
		}
		thumb := clampThumb(view*view/(view+max), view)
		return Rect{
			X: inner.Right() - w - ScrollbarInset,
			Y: inner.Y + (view-thumb)*(off/max),
			W: w, H: thumb,
		}, true
	case AxisX:
		view := inner.W
		if view <= 0 {
			return Rect{}, false
		}
		thumb := clampThumb(view*view/(view+max), view)
		return Rect{
			X: inner.X + (view-thumb)*(off/max),
			Y: inner.Bottom() - w - ScrollbarInset,
			W: thumb, H: w,
		}, true
	}
	return Rect{}, false
}

func clampThumb(thumb, view float64) float64 {
	if thumb < ScrollbarMinLength {
		thumb = ScrollbarMinLength
	}
	if thumb > view {
		thumb = view
	}
	return thumb
}

// SetScrollbarHover records whether the pointer is inside the area. The bar is
// visible while it is, and for the grace period after it leaves.
func (b *NodeBase) SetScrollbarHover(v bool) {
	if b.scrollbarHover == v {
		return
	}
	b.scrollbarHover = v
	b.MarkDirty(DirtyPaint)
}

// ScrollbarHover reports whether the pointer is inside the area.
func (b *NodeBase) ScrollbarHover() bool { return b.scrollbarHover }

// ShowScrollbar restarts the grace period, so a scroll or a pointer entering
// the area keeps the bar up for ScrollbarHideDelay.
func (b *NodeBase) ShowScrollbar() { b.scrollbarShowUntil = time.Now().Add(ScrollbarHideDelay) }

// ScrollbarVisible reports whether the bar should paint now.
func (b *NodeBase) ScrollbarVisible() bool {
	if b.HideScrollbar() {
		return false
	}
	if !b.AutoHideScrollbar() {
		return true
	}
	return b.scrollbarHover || time.Now().Before(b.scrollbarShowUntil)
}
