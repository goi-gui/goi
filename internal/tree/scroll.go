package tree

import (
	"math"
	"slices"
	"time"
)

// ScrollAlign is where a child lands in the viewport when a scroll shortcut
// brings it there.
type ScrollAlign string

// Scroll alignments: the child's leading edge at the start, its middle in the
// middle, or its trailing edge at the end.
const (
	ScrollStart  ScrollAlign = "start"
	ScrollCenter ScrollAlign = "center"
	ScrollEnd    ScrollAlign = "end"
)

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
	case "List", "Menu", "NavigationDrawer", "Dialog", "DatePicker":
		// These scroll whenever their content overflows; they have no `scroll`
		// switch to turn it off.
		return AxisY
	}
	if !b.scroll.Get() {
		return AxisNone
	}
	switch b.Node().Kind() {
	case "Row", "TitleBar", "Carousel":
		return AxisX
	case "Flex":
		if b.Node().(*Flex).Direction.Get() != "column" {
			return AxisX
		}
		return AxisY
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
		// A scrolling box only cuts what actually overflows it. A column that
		// happens to be a scroll container now clips nothing until its content
		// is taller than the frame, so a child's shadow or focus ring at the
		// edge is not cut for no reason.
		if sx, sy := b.ScrollShift(); sx != 0 || sy != 0 {
			return true
		}
		return b.ScrollMaxFor(AxisX) > 0 || b.ScrollMaxFor(AxisY) > 0
	}
	switch b.Node().Kind() {
	case "Column", "Row", "Flex", "TitleBar", "Positioned", "Wrap", "Grid", "Toolbar",
		"ButtonGroup", "Popover":
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
	b.scrollAnim = nil
	b.setScrollOffsetRaw(axis, v)
}

// SetScrollOffsetForBy scrolls one axis and says who moved it. Input uses
// byUser true for a wheel, drag, fling or scrollbar; a design or Go shortcut
// uses false.
func (b *NodeBase) SetScrollOffsetForBy(axis Axis, v float64, byUser bool) {
	b.scrollAnim = nil
	b.setScrollOffsetRawBy(axis, v, byUser)
}

// setScrollOffsetRaw writes the offset without stopping a smooth scroll, which
// is what the animation's own frames use.
func (b *NodeBase) setScrollOffsetRaw(axis Axis, v float64) {
	b.setScrollOffsetRawBy(axis, v, false)
}

func (b *NodeBase) setScrollOffsetRawBy(axis Axis, v float64, byUser bool) {
	oldX, oldY := b.ScrollOffsetFor(AxisX), b.ScrollOffsetFor(AxisY)
	switch axis {
	case AxisX:
		b.scrollX = v
	case AxisY:
		b.scrollY = v
	default:
		return
	}
	dirty := DirtyPaint
	if b.Node().Kind() == "Carousel" {
		// A carousel's card widths follow the offset, so every scroll frame is
		// also a layout frame.
		dirty = DirtyLayout
	}
	b.MarkDirty(dirty)
	b.scrollMoved(oldX, oldY, byUser)
}

// scrollMoved reports a real offset change and updates the edge and auto-scroll
// state. The clamped offsets are what the design sees.
func (b *NodeBase) scrollMoved(oldX, oldY float64, byUser bool) {
	x, y := b.ScrollOffsetFor(AxisX), b.ScrollOffsetFor(AxisY)
	if x == oldX && y == oldY {
		return
	}
	b.Scroll.Emit(ScrollEvent{X: x, Y: y, ByUser: byUser})
	b.updateScrollEdges()
	if !byUser || !b.autoScroll.Get() {
		return
	}
	axis := b.primaryScrollAxis()
	if axis == AxisNone {
		return
	}
	max := b.ScrollMaxFor(axis)
	off := b.ScrollOffsetFor(axis)
	// A user scroll away from the end pauses the follow; reaching the end
	// starts it again even without the end event's help.
	b.autoScrollPaused = max > 0 && off < max-0.5
}

// updateScrollEdges fires the start and end events once when the primary axis
// reaches an edge. A fresh node starts at the top, so the first move down does
// not report a start.
func (b *NodeBase) updateScrollEdges() {
	axis := b.primaryScrollAxis()
	if axis == AxisNone {
		return
	}
	max := b.ScrollMaxFor(axis)
	off := b.ScrollOffsetFor(axis)
	atStart := off <= 0.5
	atEnd := max <= 0 || off >= max-0.5
	if atStart && !b.scrollAtStart {
		b.ScrollStart.Emit(struct{}{})
	}
	if atEnd && !b.scrollAtEnd {
		b.ScrollEnd.Emit(struct{}{})
	}
	b.scrollAtStart, b.scrollAtEnd = atStart, atEnd
}

// ScrollTo jumps along the node's primary axis: down a column, sideways along a
// row, and down a two-axis table. It does nothing on a node that does not scroll.
func (b *NodeBase) ScrollTo(v float64) {
	axis := b.primaryScrollAxis()
	if axis == AxisNone {
		return
	}
	b.SetScrollOffsetFor(axis, v)
	b.ShowScrollbar()
}

// ScrollToSmooth eases to v over d, starting from the offset it shows now. A
// duration of zero or less jumps. It does nothing on a node that does not scroll.
func (b *NodeBase) ScrollToSmooth(v float64, d time.Duration) {
	b.scrollToSmoothBy(v, d, false)
}

func (b *NodeBase) scrollToSmoothBy(v float64, d time.Duration, byUser bool) {
	axis := b.primaryScrollAxis()
	if axis == AxisNone {
		return
	}
	if d <= 0 {
		b.SetScrollOffsetForBy(axis, v, byUser)
		return
	}
	from := b.ScrollOffsetFor(axis)
	if from == v {
		b.SetScrollOffsetForBy(axis, v, byUser)
		return
	}
	b.scrollAnim = &scrollAnimation{axis: axis, from: from, to: v, dur: d, byUser: byUser}
	b.listTransition()
	b.ShowScrollbar()
	b.MarkDirty(DirtyPaint)
}

// AutoScroll turns following the end of the content on or off and scrolls to
// it now. While it is on, content that grows keeps the newest row in view;
// scrolling away pauses that until the reader returns to the end.
func (b *NodeBase) AutoScroll(on bool) { b.setAutoScroll(on, 0) }

// AutoScrollSmooth is AutoScroll with an eased move over d. The duration stays
// on the node, so later content growth uses the same ease.
func (b *NodeBase) AutoScrollSmooth(on bool, d time.Duration) { b.setAutoScroll(on, d) }

// AutoScrollEnabled reports whether the node follows its end.
func (b *NodeBase) AutoScrollEnabled() bool { return b.autoScroll.Get() }

func (b *NodeBase) setAutoScroll(on bool, d time.Duration) {
	b.autoScroll.Set(on)
	b.autoScrollDur = d
	b.autoScrollPaused = false
	b.autoScrollForce = on
	if !on {
		return
	}
	if max := b.ScrollMaxFor(b.primaryScrollAxis()); max > 0 {
		b.scrollToEnd(d)
		return
	}
	// The first layout has not measured the content yet; force one so the
	// pending follow can land.
	b.MarkDirty(DirtyLayout)
}

func (b *NodeBase) scrollToEnd(d time.Duration) {
	axis := b.primaryScrollAxis()
	if axis == AxisNone {
		return
	}
	max := b.ScrollMaxFor(axis)
	if max <= 0 {
		return
	}
	if d > 0 {
		b.scrollToSmoothBy(max, d, false)
		return
	}
	b.SetScrollOffsetFor(axis, max)
}

// ScrollAtEnd reports whether the primary axis is at its end. A short or
// non-scrolling area counts as both at the start and at the end.
func (b *NodeBase) ScrollAtEnd() bool {
	axis := b.primaryScrollAxis()
	if axis == AxisNone {
		return true
	}
	max := b.ScrollMaxFor(axis)
	return max <= 0 || b.ScrollOffsetFor(axis) >= max-0.5
}

// ScrollAtStart reports whether the primary axis is at its start.
func (b *NodeBase) ScrollAtStart() bool {
	return b.ScrollOffsetFor(b.primaryScrollAxis()) <= 0.5
}

// ApplyAutoScroll follows content growth for every node with autoScroll on. It
// runs after layout, once the new content has a size. A pause set by a user
// scroll clears when the reader reaches the end again.
func (t *Tree) ApplyAutoScroll() {
	t.Walk(func(n Node) {
		b := n.Base()
		axis := b.primaryScrollAxis()
		if axis == AxisNone {
			return
		}
		max := b.ScrollMaxFor(axis)
		// Content can shrink under the reader, which clamps the offset without
		// a write. The edges still have to follow that.
		if max != b.autoScrollLastMax {
			b.updateScrollEdges()
		}
		if b.autoScroll.Get() && (b.autoScrollForce || (max > b.autoScrollLastMax+0.5 && !b.autoScrollPaused)) {
			b.scrollToEnd(b.autoScrollDur)
		}
		b.autoScrollLastMax = max
		b.autoScrollForce = false
	})
}

// ScrollToNthChild scrolls the nth visible direct child into view, counting
// from one. The alignment picks its leading edge, its middle or its trailing
// edge; no alignment means start.
func (b *NodeBase) ScrollToNthChild(n int, align ...ScrollAlign) bool {
	off, ok := b.nthChildOffset(n, firstAlign(align))
	if !ok {
		return false
	}
	b.ScrollTo(off)
	return true
}

// ScrollToNthChildSmooth is ScrollToNthChild eased over d. The alignment is
// optional and follows the duration, so a start-aligned jump stays
// ScrollToNthChildSmooth(5, 300*time.Millisecond).
func (b *NodeBase) ScrollToNthChildSmooth(n int, d time.Duration, align ...ScrollAlign) bool {
	off, ok := b.nthChildOffset(n, firstAlign(align))
	if !ok {
		return false
	}
	b.ScrollToSmooth(off, d)
	return true
}

// ScrollToChild scrolls the element with id, anywhere inside this node, into
// view. The alignment picks its leading edge, its middle or its trailing edge;
// no alignment means start. It reports false when the id is not inside this
// node or the node does not scroll.
func (b *NodeBase) ScrollToChild(id string, align ...ScrollAlign) bool {
	off, ok := b.childOffset(id, firstAlign(align))
	if !ok {
		return false
	}
	b.ScrollTo(off)
	return true
}

// ScrollToChildSmooth is ScrollToChild eased over d. The alignment is optional
// and follows the duration.
func (b *NodeBase) ScrollToChildSmooth(id string, d time.Duration, align ...ScrollAlign) bool {
	off, ok := b.childOffset(id, firstAlign(align))
	if !ok {
		return false
	}
	b.ScrollToSmooth(off, d)
	return true
}

// firstAlign is the requested alignment, or start when the caller named none.
func firstAlign(align []ScrollAlign) ScrollAlign {
	if len(align) > 0 {
		return align[0]
	}
	return ScrollStart
}

// primaryScrollAxis is the one axis the shortcut helpers move: a table's
// vertical axis, and the node's own direction otherwise.
func (b *NodeBase) primaryScrollAxis() Axis {
	axis := b.ScrollAxis()
	if axis == AxisBoth {
		axis = AxisY
	}
	return axis
}

// nthChildOffset aligns one visible direct child in the viewport.
func (b *NodeBase) nthChildOffset(n int, align ScrollAlign) (float64, bool) {
	axis := b.primaryScrollAxis()
	if axis == AxisNone || n < 1 {
		return 0, false
	}
	i := 0
	for _, k := range b.kids {
		if !k.Visible() {
			continue
		}
		i++
		if i == n {
			return b.alignOffset(axis, k, align), true
		}
	}
	return 0, false
}

// childOffset finds an element by id and aligns it when it is inside b.
func (b *NodeBase) childOffset(id string, align ScrollAlign) (float64, bool) {
	axis := b.primaryScrollAxis()
	if axis == AxisNone || b.tree == nil {
		return 0, false
	}
	n, ok := b.tree.ByID(id)
	if !ok {
		return 0, false
	}
	k := n.Base()
	if k == b || !b.holds(k) {
		return 0, false
	}
	return b.alignOffset(axis, k, align), true
}

// holds reports whether k is somewhere below b.
func (b *NodeBase) holds(k *NodeBase) bool {
	for n := k.parent; n != nil; n = n.parent {
		if n == b {
			return true
		}
	}
	return false
}

// alignOffset is the offset that puts k at the requested place in b's viewport
// before scrolling.
func (b *NodeBase) alignOffset(axis Axis, k *NodeBase, align ScrollAlign) float64 {
	be := b.BorderEdges()
	start, size := k.bounds.Y, k.bounds.H
	viewStart, view := b.bounds.Y+be.Top, b.bounds.H-be.Top-be.Bottom
	if axis == AxisX {
		start, size = k.bounds.X, k.bounds.W
		viewStart, view = b.bounds.X+be.Left, b.bounds.W-be.Left-be.Right
	}
	switch align {
	case ScrollCenter:
		return start + size/2 - (viewStart + view/2)
	case ScrollEnd:
		return start + size - (viewStart + view)
	default:
		return start - viewStart
	}
}

// scrollAnimation is one smooth scroll. It is written every frame by
// AdvanceTransitions, so a flight does not need a tween of its own.
type scrollAnimation struct {
	axis         Axis
	from, to     float64
	elapsed, dur time.Duration
	byUser       bool
}

// advanceScroll moves a smooth scroll one frame forward and reports whether it
// is still running.
func (b *NodeBase) advanceScroll(elapsed time.Duration) bool {
	a := b.scrollAnim
	if a == nil {
		return false
	}
	a.elapsed += elapsed
	p := 1.0
	if a.dur > 0 {
		p = float64(a.elapsed) / float64(a.dur)
	}
	if p > 1 {
		p = 1
	}
	// The same cubic ease-out a transition uses by default.
	e := 1 - (1-p)*(1-p)*(1-p)
	b.setScrollOffsetRawBy(a.axis, a.from+(a.to-a.from)*e, a.byUser)
	b.ShowScrollbar()
	if p >= 1 {
		b.scrollAnim = nil
	}
	return true
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

// SnapProximity is how close a stop has to be for `proximity` to take it, as a
// share of the viewport. A gesture that ends within a quarter of the box coasts
// onto the point; one that ends further away is left where the reader put it.
const SnapProximity = 0.25

// SnapTargets lists the offsets along axis where a child's snap area lines up
// with the viewport, sorted and clamped to what the content allows. A child's
// scrollSnapAlign picks the edge: start, center or end, inset by scrollPadding
// for the two edges the padding can move. Children that do not scroll with the
// content (a sticky table header) and invisible ones are skipped.
func (b *NodeBase) SnapTargets(axis Axis) []float64 {
	if !b.scrollsOn(axis) {
		return nil
	}
	be := b.BorderEdges()
	viewStart, view := b.bounds.Y+be.Top, b.bounds.H-be.Top-be.Bottom
	if axis == AxisX {
		viewStart, view = b.bounds.X+be.Left, b.bounds.W-be.Left-be.Right
	}
	if view <= 0 {
		return nil
	}
	pad := b.SnapPadding()
	max := b.ScrollMaxFor(axis)
	var out []float64
	for _, k := range b.kids {
		if !k.Visible() || (axis == AxisY && b.fixedChild(k)) {
			continue
		}
		lo, size := k.Bounds().Y, k.Bounds().H
		if axis == AxisX {
			lo, size = k.Bounds().X, k.Bounds().W
		}
		var off float64
		switch k.Base().SnapAlign() {
		case "start":
			off = lo - viewStart - pad
		case "center":
			off = lo + size/2 - viewStart - view/2
		case "end":
			off = lo + size - viewStart - view + pad
		default:
			continue
		}
		off = min(maxf(off, 0), max)
		if !slices.Contains(out, off) {
			out = append(out, off)
		}
	}
	slices.Sort(out)
	return out
}

// SnapOffset returns the snap target a gesture ending at off should settle on.
// `mandatory` always answers; `proximity` answers only when the point is within
// SnapProximity of the viewport, and leaves the offset alone otherwise.
func (b *NodeBase) SnapOffset(axis Axis, off float64) (float64, bool) {
	targets := b.SnapTargets(axis)
	if len(targets) == 0 {
		return 0, false
	}
	best, bestD := 0.0, math.Inf(1)
	for _, t := range targets {
		if d := math.Abs(t - off); d < bestD {
			best, bestD = t, d
		}
	}
	if b.SnapType() == "proximity" {
		be := b.BorderEdges()
		view := b.bounds.H - be.Top - be.Bottom
		if axis == AxisX {
			view = b.bounds.W - be.Left - be.Right
		}
		if bestD > view*SnapProximity {
			return 0, false
		}
	}
	return best, true
}

// SnapNext returns the first target past off in the direction dir (+1 forward,
// -1 back), which is what one notch of a wheel moves on a snapping container.
// At the end it reports the last target, so a wheel step never overshoots.
func (b *NodeBase) SnapNext(axis Axis, off, dir float64) (float64, bool) {
	targets := b.SnapTargets(axis)
	if len(targets) == 0 {
		return 0, false
	}
	if dir >= 0 {
		for _, t := range targets {
			if t > off+0.5 {
				return t, true
			}
		}
		return targets[len(targets)-1], true
	}
	for i := len(targets) - 1; i >= 0; i-- {
		if targets[i] < off-0.5 {
			return targets[i], true
		}
	}
	return targets[0], true
}

// maxf is the larger of two floats.
func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
