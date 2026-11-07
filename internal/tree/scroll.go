package tree

import "time"

// Axis is a scroll direction.
type Axis uint8

// Scroll axes.
const (
	AxisNone Axis = iota
	AxisX
	AxisY
)

// ScrollAxis reports the direction a node scrolls, if any.
//
// A node scrolls when it sets `scroll: true`; a Row then scrolls horizontally
// and everything else vertically, so the axis follows the container's flow.
// The Material components that overflow their box scroll without the property,
// and a carousel scrolls sideways.
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
	if b.Node().Kind() == "Row" || b.Node().Kind() == "TitleBar" {
		return AxisX
	}
	return AxisY
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
// the children reach past the content box.
func (b *NodeBase) ScrollMax() float64 {
	axis := b.ScrollAxis()
	if axis == AxisNone {
		return 0
	}
	// Children are positioned from the outer box, so only the far edge inset
	// shrinks the visible area: content may run into the last padding.
	pe, bw := b.PaddingEdges(), b.BorderWidth()
	view := b.bounds.H - pe.Bottom - bw
	if axis == AxisX {
		view = b.bounds.W - pe.Right - bw
	}
	end := 0.0
	for _, k := range b.kids {
		if !k.Visible() {
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
	var v float64
	switch b.ScrollAxis() {
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
	if m := b.ScrollMax(); v > m {
		return m
	}
	return v
}

// SetScrollOffset scrolls the node, storing the raw value: reads clamp, so the
// offset survives content shrinking and growing back.
func (b *NodeBase) SetScrollOffset(v float64) {
	switch b.ScrollAxis() {
	case AxisX:
		b.scrollX = v
	case AxisY:
		b.scrollY = v
	default:
		return
	}
	b.MarkDirty(DirtyPaint)
}

// Scrollbar metrics: a 4dp pill, 2dp in from the trailing edge, never shorter
// than 32dp so a long document still has something to grab.
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
// the bar a reader grabs is exactly the bar that was drawn.
func (b *NodeBase) ScrollbarThumb(inner Rect) (Rect, bool) {
	axis := b.ScrollAxis()
	if axis == AxisNone {
		return Rect{}, false
	}
	max := b.ScrollMax()
	if max <= 0 {
		return Rect{}, false
	}
	off := b.ScrollOffset()
	switch axis {
	case AxisY:
		view := inner.H
		if view <= 0 {
			return Rect{}, false
		}
		thumb := clampThumb(view*view/(view+max), view)
		return Rect{
			X: inner.Right() - ScrollbarThickness - ScrollbarInset,
			Y: inner.Y + (view-thumb)*(off/max),
			W: ScrollbarThickness, H: thumb,
		}, true
	case AxisX:
		view := inner.W
		if view <= 0 {
			return Rect{}, false
		}
		thumb := clampThumb(view*view/(view+max), view)
		return Rect{
			X: inner.X + (view-thumb)*(off/max),
			Y: inner.Bottom() - ScrollbarThickness - ScrollbarInset,
			W: thumb, H: ScrollbarThickness,
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
