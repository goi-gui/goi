package ui

import (
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/platform"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// interactor turns platform input into tree events. It runs on the UI
// goroutine; every method is called from the platform callback during
// PollEvents, so no locking is needed.
type interactor struct {
	app  *App
	tree *tree.Tree

	hover   tree.Node
	pressed tree.Node
	// pressX/Y remember where the button went down, for sliders.
	pressX, pressY float64
	// dragging marks a slider drag.
	dragging bool
	focus    *tree.Component
	// kbd is the node with keyboard focus. It differs from focus: focus is the
	// text field that receives characters, kbd is whatever Enter would press.
	kbd tree.Node

	// cursor is the last pointer position; the wheel scrolls whatever is under it.
	cursorX, cursorY float64
	// A press inside a scroll area may turn into a drag, which cancels the click.
	scrollNode           tree.Node
	dragFromX, dragFromY float64 // where the press landed, for the drag slop
	dragLastX, dragLastY float64 // the previous move, so a drag scrolls by steps
	scrolled             bool

	// ripples are the live press animations.
	ripples []ripple

	// textField is the field the pointer went down on, and selecting marks a
	// drag that extends its selection. clickCount detects double and triple
	// clicks by time and distance.
	textField *tree.Component
	selecting bool
	// dragX is the x used for caret mapping while dragging a selection. It
	// keeps moving on its own when the pointer sits past the field edge, which
	// is what scrolls a long value instead of stopping at the visible end.
	dragX float64
	// dragY is the vertical half of the same mapping, which a multiline field
	// needs to tell one wrapped line from the next.
	dragY      float64
	clickCount int
	lastClick  time.Time
	lastClickX float64
	lastClickY float64
	lastField  *tree.Component
	// The same counters for a selectable Text, which needs word and paragraph
	// selections but is not a field.
	textClickCount int
	textClickAt    time.Time
	textClickX     float64
	textClickY     float64
	textClickNode  tree.Node

	// Clipboard hooks; nil uses the platform clipboard.
	getClipboard func() string
	setClipboard func(string)

	// measure shapes a string for caret and selection geometry. The driver sets
	// it to the same measurer the painter uses; nil falls back to an estimate.
	measure func(s string, size float64, weight int) float64

	// setCursor changes the native pointer shape; nil in headless tests.
	setCursor func(platform.Cursor)

	// resizeEdges reports that the window is borderless, so the outer pixels
	// resize it instead of belonging to the client area.
	resizeEdges bool

	viewportW, viewportH float64

	// textSel is the selectable node the pointer last selected, so Ctrl+C can
	// copy it after the pointer is released.
	textSel tree.Node

	// Fling keeps a wheel or touchpad scroll moving after the gesture ends.
	flingNode tree.Node
	flingV    float64 // logical pixels per second
	lastWheel time.Time

	// scrollbar is the node whose bar the pointer grabbed, with the thumb
	// geometry frozen at the press so a resize mid-drag cannot shift it. grab is
	// the distance from the pointer to the thumb's leading edge.
	scrollbar     tree.Node
	scrollbarAxis tree.Axis
	scrollbarIn   tree.Rect
	scrollbarGrab float64
	// hoverScroll is the area the pointer is inside, so its bar follows the
	// pointer; hideTimer asks for one repaint when the grace period ends.
	hoverScroll tree.Node
	hideTimer   *time.Timer
}

// comp returns the Material component behind a node, if it has one.
func comp(n tree.Node) *tree.Component {
	if n == nil {
		return nil
	}
	if m, ok := n.(interface{ M3() *tree.Component }); ok {
		return m.M3()
	}
	return nil
}

// interactive reports whether a component kind reacts to the pointer.
func interactive(n tree.Node) bool {
	switch n.Kind() {
	case "Button", "IconButton", "FAB", "FABMenu", "SplitButton", "SegmentedButton",
		"Chip", "Checkbox", "RadioButton", "Switch", "Slider", "TextField",
		"SearchBar", "ListItem", "MenuItem", "Tab", "Card", "NavigationItem",
		"DatePicker", "TimePicker":
		return true
	}
	return false
}

// clickable reports whether a press on n activates it: an interactive
// component, or any node the app attached a click handler to.
func clickable(n tree.Node) bool {
	if c := comp(n); c != nil {
		if c.Disabled.Get() {
			return false
		}
		if interactive(n) {
			return true
		}
	}
	return n.Base().Click.Len() > 0
}

// dragRegionAt reports whether n or an ancestor moves the window on a press.
func dragRegionAt(n tree.Node) bool {
	for ; n != nil; n = n.Base().Parent() {
		if d, ok := n.(interface{ DragRegion() bool }); ok && d.DragRegion() {
			return true
		}
	}
	return false
}

// resizeMargin is how close to the edge a borderless window starts resizing.
const resizeMargin = 6

// chromeHitTest tells the platform what a press at x,y means. A borderless
// window owns its title bar and edges, so the compositor is asked to move the
// window on a drag area and to resize it on the outer pixels; such a press
// never reaches the input layer, which is why nothing here has to track it.
func (i *interactor) chromeHitTest(x, y float64) platform.HitTest {
	hit := i.hit(x, y)
	// A real control wins over the frame: a window button parked in the corner
	// is still a button.
	if hit != nil && (clickable(hit) || fieldAt(hit) != nil || selectableAt(hit) != nil) {
		return platform.HitTestNormal
	}
	if i.resizeEdges {
		if e := i.resizeEdge(x, y); e != platform.HitTestNormal {
			return e
		}
	}
	if dragRegionAt(hit) {
		return platform.HitTestDrag
	}
	return platform.HitTestNormal
}

// resizeEdge maps a point in the outer ring of the window to the edge it grabs.
func (i *interactor) resizeEdge(x, y float64) platform.HitTest {
	w, h := i.windowW(), i.windowH()
	if w <= 0 || h <= 0 || x < 0 || y < 0 || x >= w || y >= h {
		return platform.HitTestNormal
	}
	left, right := x < resizeMargin, x >= w-resizeMargin
	top, bottom := y < resizeMargin, y >= h-resizeMargin
	switch {
	case top && left:
		return platform.HitTestResizeTopLeft
	case top && right:
		return platform.HitTestResizeTopRight
	case bottom && left:
		return platform.HitTestResizeBottomLeft
	case bottom && right:
		return platform.HitTestResizeBottomRight
	case top:
		return platform.HitTestResizeTop
	case bottom:
		return platform.HitTestResizeBottom
	case left:
		return platform.HitTestResizeLeft
	case right:
		return platform.HitTestResizeRight
	}
	return platform.HitTestNormal
}

// event logs one input event when DEBUG_UI is on. Pointer motion is left out on
// purpose: it fires at the display rate and would drown every other line.
func (i *interactor) event(name string, fields ...any) {
	if i.app == nil || !i.app.debugUI {
		return
	}
	logg().Debug(logCtx, "ui: input", append([]any{"event", name}, fields...)...)
}

func (i *interactor) MouseMove(x, y float64) {
	i.cursorX, i.cursorY = x, y
	if i.scrollbar != nil {
		i.dragScrollbar(x, y)
		i.pointer(platform.CursorPointer)
		return
	}
	hit := i.hit(x, y)
	i.updateScrollHover(hit)
	if i.selecting && i.textSel != nil {
		i.pointer(platform.CursorText)
		a, _ := selRange(i.textSel)
		setSel(i.textSel, a, i.selIndexAt(i.textSel, x, y))
		i.markDirty()
		return
	}
	i.pointerAt(hit)
	if i.selecting && i.textField != nil {
		i.dragX, i.dragY = x, y
		i.caretFromPoint(i.textField, x, y, true)
		return
	}
	if i.dragScroll(x, y) {
		return
	}
	if hit == i.hover && !i.dragging {
		return
	}
	i.hover = hit
	changed := false
	i.tree.Walk(func(n tree.Node) {
		c := comp(n)
		if c == nil {
			return
		}
		want := isDescendantOf(hit, n) && interactive(n)
		if c.Hover != want {
			c.Hover = want
			c.Base().MarkDirty(tree.DirtyPaint)
			changed = true
		}
	})
	if i.dragging {
		i.drag(x, y)
	}
	if changed {
		i.markDirty()
	}
}

// pointer sets the native pointer shape when a driver is attached.
func (i *interactor) pointer(c platform.Cursor) {
	if i.setCursor != nil {
		i.setCursor(c)
	}
}

// pointerAt picks the shape M3 shows: an I-beam over text a reader can edit or
// select, a pointing hand over anything clickable.
func (i *interactor) pointerAt(hit tree.Node) {
	if hit == nil {
		i.pointer(platform.CursorArrow)
		return
	}
	if codeCopyAt(hit, i.cursorX, i.cursorY) != nil {
		i.pointer(platform.CursorPointer)
		return
	}
	if selectableAt(hit) != nil {
		i.pointer(platform.CursorText)
		return
	}
	if f := fieldAt(hit); f != nil {
		if !f.Disabled.Get() {
			i.pointer(platform.CursorText)
			return
		}
		i.pointer(platform.CursorArrow)
		return
	}
	if clickable(hit) {
		i.pointer(platform.CursorPointer)
		return
	}
	i.pointer(platform.CursorArrow)
}

func (i *interactor) MouseDown(x, y float64, button int, shift bool) {
	i.event("MouseDown", "x", x, "y", y, "button", button, "shift", shift)
	i.cursorX, i.cursorY = x, y
	hit := i.hit(x, y)
	i.pressX, i.pressY = x, y
	i.pressed = nil
	i.dragging = false
	i.scrolled = false
	i.textField = nil
	i.selecting = false
	i.clearTextSelection()
	i.dragFromX, i.dragFromY = x, y
	i.dragLastX, i.dragLastY = x, y
	i.scrollNode = nil
	// A press on a visible scrollbar grabs the thumb before anything else: the
	// bar is drawn on top of the content, so it has to receive the press first.
	if button == 0 {
		if n, in := i.scrollbarAt(x, y); n != nil {
			i.scrollbar = n
			i.scrollbarAxis = n.Base().ScrollAxis()
			i.scrollbarIn = in
			thumb, _ := n.Base().ScrollbarThumb(in)
			if i.scrollbarAxis == tree.AxisX {
				i.scrollbarGrab = x - thumb.X
			} else {
				i.scrollbarGrab = y - thumb.Y
			}
			i.showScrollbar(n)
			return
		}
	}
	field := fieldAt(hit)
	// A press in a scroll area can become a drag; a slider keeps the pointer,
	// and so does a text field, where the drag selects text.
	if hit != nil && hit.Kind() != "Slider" && field == nil {
		i.scrollNode = scrollableAt(hit)
	}

	// A selectable Text starts a selection drag before any container click:
	// reading a paragraph should not also press the card around it. The press
	// consumes the gesture, so MouseUp has nothing to activate.
	// A code block's copy button sits inside its selectable text, so it is
	// checked before the selection starts: the press copies and stops.
	if code := codeCopyAt(hit, x, y); code != nil && button == 0 {
		i.textSel = nil
		i.setFocus(nil)
		i.setKbd(nil)
		i.putClipboard(code.Source())
		i.markDirty()
		return
	}

	if t := selectableAt(hit); t != nil && button == 0 {
		i.setFocus(nil)
		i.setKbd(nil)
		i.scrollNode = nil
		i.pressTextNode(t, x, y, shift)
		return
	}

	// Text fields take focus wherever the press lands inside them. Only the
	// primary button moves the caret or starts a selection drag.
	if field != nil {
		i.setKbd(field.Base().Node())
		i.setFocus(field)
		if button == 0 {
			i.textField = field
			i.selecting = true
			i.dragX = x
			i.pressText(field, x, y, shift)
		}
		return
	}
	i.setFocus(nil)
	i.setKbd(nil)

	if hit == nil {
		return
	}
	if clickable(hit) {
		i.setKbd(hit)
		i.pressed = hit
		if c := comp(hit); c != nil {
			c.Pressed = true
			c.Base().MarkDirty(tree.DirtyPaint)
			if hit.Kind() == "Slider" {
				i.dragging = true
				i.drag(x, y)
				i.markDirty()
				return
			}
			i.startRipple(hit, x, y)
		}
		i.markDirty()
		return
	}
	// Tapping a container selects its nearest selectable child.
	if sel := selectableChildAt(i.tree, hit); sel != nil {
		i.pressed = sel
	}
	_ = button
}

func (i *interactor) MouseUp(x, y float64, button int) {
	i.event("MouseUp", "x", x, "y", y, "button", button)
	defer func() {
		i.pressed = nil
		i.dragging = false
		i.scrollNode = nil
		i.scrolled = false
		i.textField = nil
		i.selecting = false
		i.scrollbar = nil
	}()
	if i.pressed == nil {
		i.dragging = false
		return
	}
	target := i.pressed
	c := comp(target)
	if c != nil && c.Pressed {
		c.Pressed = false
		c.Base().MarkDirty(tree.DirtyPaint)
	}
	i.dragging = false

	hit := i.hit(x, y)
	if target.Kind() == "Slider" {
		i.markDirty()
		return
	}
	if !isDescendantOf(hit, target) && hit != target {
		return // press and release must land on the same component
	}

	i.activate(target, c, x, y)
	_ = button
}

// activate runs the component's click behaviour.
func (i *interactor) activate(n tree.Node, c *tree.Component, x, y float64) {
	if c == nil {
		// A plain node with a click handler (a scrim, a custom hit area).
		n.Base().Click.Emit(tree.ClickEvent{X: x, Y: y, Button: 0})
		i.markDirty()
		return
	}
	if c.Disabled.Get() {
		return
	}
	switch n.Kind() {
	case "Checkbox", "Switch":
		c.Checked.Set(!c.Checked.Get())
		c.Toggle.Emit(c.Checked.Get())
	case "RadioButton":
		if !c.Checked.Get() {
			c.Checked.Set(true)
			c.Toggle.Emit(true)
		}
	case "Chip":
		if c.Variant.Get() == "filter" || c.Variant.Get() == "input" {
			c.Selected.Set(!c.Selected.Get())
			c.Toggle.Emit(c.Selected.Get())
		}
	case "Tab", "MenuItem", "ListItem":
		if p := n.Base().Parent(); p != nil {
			if pc := comp(p); pc != nil {
				for idx, kid := range p.Base().Kids() {
					if kid.Base() == n.Base() {
						pc.SelectedIndex = idx
						pc.Select.Emit(idx)
					}
				}
			}
		}
		if n.Kind() != "MenuItem" && n.Kind() != "ListItem" {
			c.Selected.Set(true)
		}
	case "SegmentedButton":
		i.activateSegment(n, c)
	case "NavigationItem":
		i.activateDestination(n, c)
	case "DatePicker":
		i.pickDate(n, c, x, y)
	case "TimePicker":
		i.pickTime(n, c, x, y)
	}
	c.Click.Emit(tree.ClickEvent{X: x, Y: y})
	i.markDirty()
}

// pressTextNode places a selectable Text's selection. A second click inside the
// word selects the word and a third selects the paragraph, which is the desktop
// convention; the drag after a multi-click extends from there instead of
// collapsing to the caret.
func (i *interactor) pressTextNode(n tree.Node, x, y float64, shift bool) {
	now := time.Now()
	same := n == i.textClickNode && now.Sub(i.textClickAt) < 400*time.Millisecond &&
		math.Abs(x-i.textClickX) <= 6 && math.Abs(y-i.textClickY) <= 6
	if same {
		i.textClickCount++
	} else {
		i.textClickCount = 1
	}
	i.textClickAt, i.textClickX, i.textClickY, i.textClickNode = now, x, y, n

	idx := i.selIndexAt(n, x, y)
	s := selText(n)
	i.textSel = n
	switch {
	case same && i.textClickCount >= 3:
		a, b := paragraphBounds(s, idx)
		setSel(n, a, b)
		i.selecting = false
	case same && i.textClickCount == 2:
		a, b := wordBounds(s, idx)
		setSel(n, a, b)
		i.selecting = false
	default:
		setSel(n, idx, idx)
		i.selecting = true
	}
	_ = shift
	n.Base().MarkDirty(tree.DirtyPaint)
	i.markDirty()
}

// paragraphBounds expands an index to the text between the surrounding
// newlines, which is what a triple click selects.
func paragraphBounds(s string, idx int) (int, int) {
	idx = clampRune(s, idx)
	a := strings.LastIndexByte(s[:idx], '\n') + 1
	b := strings.IndexByte(s[idx:], '\n')
	if b < 0 {
		b = len(s)
	} else {
		b += idx
	}
	return a, b
}

// pickDate reads the click against the same layout the painter used: the
// arrows move the month, a day cell becomes the value.
func (i *interactor) pickDate(n tree.Node, c *tree.Component, x, y float64) {
	d := paint.DateLayoutOf(absoluteRect(n), c.Text.Get(), c.PickerMonth)
	switch {
	case hitRect(d.Prev, x, y):
		c.PickerMonth = d.Month - 1
	case hitRect(d.Next, x, y):
		c.PickerMonth = d.Month + 1
	default:
		iso, ok := d.DateAt(x, y)
		if !ok {
			return
		}
		c.Text.Set(iso)
		c.Submit.Emit(iso)
	}
	n.Base().MarkDirty(tree.DirtyPaint)
}

// pickTime sets the hour or the minutes from where the dial was clicked, and
// hands the dial over: hour first, then minutes.
func (i *interactor) pickTime(n tree.Node, c *tree.Component, x, y float64) {
	t := paint.TimeLayoutOf(absoluteRect(n), c.Text.Get(), c.PickerField)
	switch {
	case hitRect(t.HourBox, x, y):
		c.PickerField = 0
	case hitRect(t.MinuteBox, x, y):
		c.PickerField = 1
	default:
		v, field, ok := t.TimeAt(x, y)
		if !ok {
			return
		}
		c.Text.Set(v)
		c.PickerField = field
		c.Submit.Emit(v)
	}
	n.Base().MarkDirty(tree.DirtyPaint)
}

// activateDestination selects a navigation destination. Navigation is a radio
// group: picking one clears the others in the contiguous run, and the container
// reports the index so an app can swap pages. Clicking the active destination
// does nothing, which keeps a page from reloading itself.
func (i *interactor) activateDestination(n tree.Node, c *tree.Component) {
	if c.Selected.Get() {
		return
	}
	run := segmentRun(n, "NavigationItem")
	for _, sib := range run {
		if oc := comp(sib); oc != nil && oc != c && oc.Selected.Get() {
			oc.Selected.Set(false)
			oc.Base().MarkDirty(tree.DirtyPaint)
			oc.Toggle.Emit(false)
		}
	}
	c.Selected.Set(true)
	c.Toggle.Emit(true)

	p := n.Base().Parent()
	if p == nil || comp(p) == nil {
		return
	}
	// The index counts destinations only: a rail may hold a header, a spacer or
	// an action of its own, and an app maps Select straight onto its page list.
	idx := 0
	for _, kid := range p.Base().Kids() {
		if kid.Node().Kind() != "NavigationItem" {
			continue
		}
		if kid.Base() == n.Base() {
			break
		}
		idx++
	}
	pc := comp(p)
	if pc.SelectedIndex != idx {
		pc.SelectedIndex = idx
		pc.Base().MarkDirty(tree.DirtyPaint)
	}
	pc.Select.Emit(idx)
}

// activateSegment applies the group's mode to the contiguous run of segments
// around n. Multi (the default) toggles each segment independently; single
// (`variant: single`) keeps exactly one selected, like a radio group.
func (i *interactor) activateSegment(n tree.Node, c *tree.Component) {
	single := c.Variant.Get() == "single"
	if c.Selected.Get() {
		if single {
			return
		}
		c.Selected.Set(false)
		c.Toggle.Emit(false)
	} else {
		if single {
			for _, sib := range segmentRun(n, "SegmentedButton") {
				if oc := comp(sib); oc != nil && oc != c && oc.Selected.Get() {
					oc.Selected.Set(false)
					oc.Toggle.Emit(false)
				}
			}
		}
		c.Selected.Set(true)
		c.Toggle.Emit(true)
	}
	if p := n.Base().Parent(); p != nil {
		if pc := comp(p); pc != nil {
			for idx, kid := range p.Base().Kids() {
				if kid.Base() == n.Base() {
					pc.SelectedIndex = idx
					pc.Select.Emit(idx)
				}
			}
		}
	}
}

// segmentRun returns the contiguous siblings of the given kind around n. A
// sibling of another kind, such as a label, ends the run and starts a new group.
func segmentRun(n tree.Node, kind string) []tree.Node {
	p := n.Base().Parent()
	if p == nil {
		return []tree.Node{n}
	}
	kids := p.Base().Kids()
	idx := -1
	for j, k := range kids {
		if k == n.Base() {
			idx = j
			break
		}
	}
	if idx < 0 {
		return []tree.Node{n}
	}
	start, end := idx, idx
	for start > 0 && kids[start-1].Node().Kind() == kind {
		start--
	}
	for end < len(kids)-1 && kids[end+1].Node().Kind() == kind {
		end++
	}
	run := make([]tree.Node, 0, end-start+1)
	for j := start; j <= end; j++ {
		run = append(run, kids[j].Node())
	}
	return run
}

// scrollStep is the distance one wheel notch moves, in logical pixels. It is the
// usual desktop notch, so a list moves a few lines per click of the wheel.
const scrollStep = 48

// Scroll turns wheel and touchpad input into a scroll offset.
//
// A notched wheel only goes up and down; Shift turns it into a horizontal
// wheel. A touchpad reports real dx and dy, so a two-finger sideways swipe
// drives a horizontal area without Shift. Each node takes the delta along its
// own axis and nothing else: scrolling down over a sideways row under the
// pointer keeps scrolling the page, which is what the hand is doing.
//
// The step goes to the nearest scrollable ancestor of the pointer, then to the
// next one up when it has no room left. A short TextArea or a list at its end
// must not swallow the wheel, or the pointer looks stuck over that component.
func (i *interactor) Scroll(dx, dy float64, shift bool) {
	i.event("Scroll", "dx", dx, "dy", dy, "shift", shift)
	precise := preciseWheel(dx) || preciseWheel(dy)
	if !precise {
		// A notched wheel is one step on one axis. Shift turns the vertical
		// wheel into a horizontal one, which is how a sideways area is reached
		// without a touchpad, and a sideways wheel into a vertical one.
		if shift {
			dx, dy = -dy, dx
		} else if dy != 0 {
			dx = 0
		}
	}
	// SDL's x is already a scroll direction (positive to the right), while its
	// y is positive away from the user, so only y flips.
	panX, panY := dx*scrollStep, -dy*scrollStep
	if panX == 0 && panY == 0 {
		return
	}
	start := scrollableAt(i.hit(i.cursorX, i.cursorY))
	if start == nil {
		return
	}
	last, used := scrollChain(start, panX, panY)
	if last == nil {
		return
	}
	// Only a touchpad coasts. A wheel notch is one deliberate step and must
	// stop where it lands; letting it fling too made every click of the wheel
	// travel several times as far as the notch.
	if precise {
		i.startFling(last, used)
	} else {
		i.flingNode, i.flingV = nil, 0
	}
	// The content moved under a stationary pointer, so the shape may be stale.
	i.pointerAt(i.hit(i.cursorX, i.cursorY))
	i.showScrollbar(last)
	i.markDirty()
}

// scrollChain moves a gesture in logical pixels, starting at start and walking
// up the scrollable ancestors: each node takes the part along its own axis and
// the rest goes to the next one, so a child at its end does not swallow the
// gesture. It returns the outermost node that moved and the pixels it absorbed
// along its own axis.
func scrollChain(start tree.Node, panX, panY float64) (tree.Node, float64) {
	var last tree.Node
	var used float64
	for n := start; n != nil; n = n.Base().Parent() {
		b := n.Base()
		px := panY
		if b.ScrollAxis() == tree.AxisX {
			px = panX
		}
		if b.ScrollAxis() == tree.AxisNone || px == 0 {
			continue
		}
		before := b.ScrollOffset()
		after := min(max(before+px, 0), b.ScrollMax())
		if after == before {
			continue
		}
		b.SetScrollOffset(after)
		b.MarkDirty(tree.DirtyPaint)
		if b.ScrollAxis() == tree.AxisX {
			panX -= after - before
			used = after - before
		} else {
			panY -= after - before
			used = after - before
		}
		last = n
		if panX == 0 && panY == 0 {
			break
		}
	}
	return last, used
}

// preciseWheel reports whether a wheel delta comes from a touchpad or another
// smooth-scrolling device instead of a notched wheel. libinput reports a notch
// as a whole step and a touchpad as a fraction of one, so the fraction is what
// separates them.
func preciseWheel(delta float64) bool {
	if delta == 0 {
		return false
	}
	return math.Abs(delta) < 0.999 || math.Abs(delta-math.Round(delta)) > 0.001
}

// startFling turns the last touchpad step into a velocity, so a fast flick
// coasts. A step that arrives after a pause starts a new gesture instead of
// adding to a stale one.
func (i *interactor) startFling(n tree.Node, moved float64) {
	now := time.Now()
	dt := now.Sub(i.lastWheel).Seconds()
	i.lastWheel = now
	if dt <= 0 || dt > 0.25 {
		dt = 0.05 // one step is about a 20 Hz gesture
	}
	i.flingNode = n
	// A flick that is faster than this coasts further than the page is tall,
	// which reads as the scroll running away from the finger.
	i.flingV = min(max(moved/dt, -flingMaxSpeed), flingMaxSpeed)
}

// flingFriction is the exponential decay per second. 6 is a short M3-like
// coast: about 400ms to a stop from a fast flick.
const flingFriction = 6.0

// flingMaxSpeed caps a coast at flingMaxSpeed/flingFriction logical pixels,
// about 430, so a hard flick glides without throwing the content past what the
// reader aimed at.
const flingMaxSpeed = 2600.0

// tickFling advances a coasting scroll and reports whether the loop must keep
// drawing. It stops at the end of the content instead of bouncing.
func (i *interactor) tickFling(dt float64) bool {
	if i.flingNode == nil || i.flingV == 0 {
		return false
	}
	panX, panY := 0.0, i.flingV*dt
	if i.flingNode.Base().ScrollAxis() == tree.AxisX {
		panX, panY = panY, 0
	}
	last, _ := scrollChain(i.flingNode, panX, panY)
	i.flingV *= math.Exp(-flingFriction * dt)
	// Hitting an edge kills the momentum; otherwise the offset would keep
	// asking for frames while nothing moves. The chain hands a coast that
	// reaches the end of a child to the page above it.
	if last == nil || math.Abs(i.flingV) < 20 {
		i.flingNode = nil
		i.flingV = 0
		return false
	}
	i.flingNode = last
	i.showScrollbar(last)
	i.markDirty()
	return true
}

// dragScroll follows the pointer while a press inside a scroll area moves. It
// reports whether it consumed the move, which also suppresses hover.
func (i *interactor) dragScroll(x, y float64) bool {
	n := i.scrollNode
	if n == nil {
		return false
	}
	dx, dy := x-i.dragLastX, y-i.dragLastY
	i.dragLastX, i.dragLastY = x, y
	if !i.scrolled {
		if math.Abs(x-i.dragFromX) < 4 && math.Abs(y-i.dragFromY) < 4 {
			return false
		}
		// Past the slop: this is a scroll, not a click.
		i.scrolled = true
		if c := comp(i.pressed); c != nil && c.Pressed {
			c.Pressed = false
			c.Base().MarkDirty(tree.DirtyPaint)
		}
		i.pressed = nil
		i.dragging = false
	}
	// Dragging pulls the content with the hand, and it chains to the page the
	// same way the wheel does, so the pointer never sticks at a child's end.
	last, _ := scrollChain(n, -dx, -dy)
	if last == nil {
		return i.scrolled
	}
	i.flingNode, i.flingV = nil, 0
	i.markDirty()
	return true
}

// scrollableAt returns the nearest node at or above n that scrolls.
func scrollableAt(n tree.Node) tree.Node {
	for ; n != nil; n = n.Base().Parent() {
		if n.Base().ScrollAxis() != tree.AxisNone {
			return n
		}
	}
	return nil
}

// scrollbarAt finds the visible scrollbar thumb under the pointer. The thumb is
// only 4dp wide, so the grab area is padded by 8dp on each side across the axis:
// easy to hit without stealing presses from content further in.
func (i *interactor) scrollbarAt(x, y float64) (tree.Node, tree.Rect) {
	if i.tree.Root == nil {
		return nil, tree.Rect{}
	}
	var (
		found tree.Node
		inner tree.Rect
	)
	var walk func(b *tree.NodeBase, ox, oy float64, clip tree.Rect)
	walk = func(b *tree.NodeBase, ox, oy float64, clip tree.Rect) {
		if !b.Visible() || found != nil {
			return
		}
		br := b.Bounds()
		ox += br.X
		oy += br.Y
		box := tree.Rect{X: ox, Y: oy, W: br.W, H: br.H}
		innerBox := b.ContentBox(box)
		kidClip := clip
		if b.ClipsChildren() {
			kidClip = intersectRect(clip, innerBox)
		}
		sx, sy := scrollShift(b)
		kids := b.Kids()
		// Children first, so a nested area's bar wins over the page's.
		for n := len(kids) - 1; n >= 0 && found == nil; n-- {
			kb := kids[n].Bounds()
			kb.X += sx
			kb.Y += sy
			walk(kids[n], ox, oy, kidClip)
		}
		if found != nil || b.ScrollAxis() == tree.AxisNone || !b.ScrollbarVisible() {
			return
		}
		thumb, ok := b.ScrollbarThumb(b.ScrollViewport(box))
		if !ok {
			return
		}
		const pad = 8.0
		hit := thumb
		if b.ScrollAxis() == tree.AxisX {
			hit.Y -= pad
			hit.H += pad * 2
		} else {
			hit.X -= pad
			hit.W += pad * 2
		}
		if hit.Contains(x, y) && clip.Contains(x, y) {
			found, inner = b.Node(), innerBox
		}
	}
	walk(i.tree.Root.Base(), 0, 0, tree.Rect{X: 0, Y: 0, W: i.windowW(), H: i.windowH()})
	return found, inner
}

// dragScrollbar moves the grabbed thumb with the pointer. The thumb is the
// handle, so the content follows it 1:1 over the track instead of by a delta.
func (i *interactor) dragScrollbar(x, y float64) {
	n := i.scrollbar
	if n == nil {
		return
	}
	b := n.Base()
	thumb, ok := b.ScrollbarThumb(i.scrollbarIn)
	if !ok {
		return
	}
	var track, pos float64
	if i.scrollbarAxis == tree.AxisX {
		track = i.scrollbarIn.W - thumb.W
		pos = x - i.scrollbarGrab - i.scrollbarIn.X
	} else {
		track = i.scrollbarIn.H - thumb.H
		pos = y - i.scrollbarGrab - i.scrollbarIn.Y
	}
	if track <= 0 {
		return
	}
	frac := pos / track
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}
	b.SetScrollOffset(frac * b.ScrollMax())
	i.flingNode, i.flingV = nil, 0
	i.showScrollbar(n)
	i.markDirty()
}

// updateScrollHover keeps the area under the pointer marked, so its bar is up
// while the pointer is inside and fades on the grace period after it leaves.
func (i *interactor) updateScrollHover(hit tree.Node) {
	hover := scrollableAt(hit)
	if hover == i.hoverScroll {
		return
	}
	if i.hoverScroll != nil {
		i.hoverScroll.Base().SetScrollbarHover(false)
		i.showScrollbar(i.hoverScroll)
	}
	i.hoverScroll = hover
	if hover != nil {
		hover.Base().SetScrollbarHover(true)
		i.showScrollbar(hover)
	}
}

// showScrollbar starts the grace period and asks for one repaint when it ends,
// so an auto-hiding bar disappears even if nothing else changes.
func (i *interactor) showScrollbar(n tree.Node) {
	if n == nil {
		return
	}
	n.Base().ShowScrollbar()
	n.Base().MarkDirty(tree.DirtyPaint)
	i.markDirty()
	if i.hideTimer != nil {
		i.hideTimer.Stop()
	}
	i.hideTimer = time.AfterFunc(tree.ScrollbarHideDelay, func() {
		if i.app != nil {
			i.app.Post(func() { i.markDirty() })
		}
	})
}

// MouseLeave clears the hovered area when the pointer leaves the window.
func (i *interactor) MouseLeave() {
	i.event("MouseLeave")
	if i.hoverScroll != nil {
		i.hoverScroll.Base().SetScrollbarHover(false)
		i.showScrollbar(i.hoverScroll)
		i.hoverScroll = nil
	}
	i.hover = nil
	i.markDirty()
}

func (i *interactor) Key(k platform.Key, mods platform.Mods) {
	i.event("Key", "key", int(k), "shift", mods.Shift, "ctrl", mods.Ctrl, "alt", mods.Alt, "super", mods.Super)
	i.key(toTreeKey(k), tree.Mods{Shift: mods.Shift, Ctrl: mods.Ctrl, Alt: mods.Alt, Super: mods.Super})
}

// key handles a key press in framework terms, so tests need no platform types.
func (i *interactor) key(k tree.Key, mods tree.Mods) {
	if i.app.win != nil {
		i.app.win.keys.Emit(tree.KeyEvent{Key: k, Mods: mods})
	}
	// A static text selection is not an editable focus, so its two shortcuts
	// are handled before the field path.
	if mods.Control() && k == tree.KeyC && i.copyTextSelection() {
		return
	}
	if k == tree.KeyEscape && i.textSel != nil {
		i.clearTextSelection()
		return
	}
	if k == tree.KeyTab {
		i.tabFocus(mods.Shift)
		return
	}
	if i.focus == nil && i.kbd != nil {
		// A slider takes arrow keys while it holds keyboard focus; that is the
		// M3 behaviour and the reason it is focusable at all.
		if i.kbd.Kind() == "Slider" && i.sliderKey(i.kbd, k, mods) {
			return
		}
		if k == tree.KeyEnter || k == tree.KeySpace {
			i.activateKey(i.kbd)
			return
		}
	}
	f := i.focus
	if f == nil {
		return
	}
	ctrl, shift := mods.Control(), mods.Shift
	// Alt is the other common word-delete modifier on Linux (Emacs style).
	word := ctrl || mods.Alt
	switch k {
	case tree.KeyA:
		if ctrl {
			f.SelectAll()
			f.Base().MarkDirty(tree.DirtyPaint)
		}
	case tree.KeyC:
		if ctrl {
			i.copySelection(f)
		}
	case tree.KeyX:
		if ctrl {
			if i.copySelection(f) {
				i.deleteSelection(f)
			}
		}
	case tree.KeyV:
		if ctrl {
			pasted := i.clipboardText()
			if !isMultiline(f) {
				pasted = singleLine(pasted)
			}
			i.insert(f, strings.ReplaceAll(pasted, "\r\n", "\n"))
		}
	case tree.KeyBackspace:
		if !i.deleteSelection(f) {
			if word {
				i.deleteRange(f, prevWord(f.Text.Get(), f.Caret), f.Caret)
			} else {
				i.deleteBefore(f)
			}
		}
	case tree.KeyDelete:
		if !i.deleteSelection(f) {
			if word {
				i.deleteRange(f, f.Caret, nextWordEnd(f.Text.Get(), f.Caret))
			} else {
				i.deleteAfter(f)
			}
		}
	case tree.KeyLeft:
		i.moveCaret(f, -1, shift, ctrl)
	case tree.KeyRight:
		i.moveCaret(f, 1, shift, ctrl)
	case tree.KeyUp:
		i.moveCaretVertical(f, -1, shift)
	case tree.KeyDown:
		i.moveCaretVertical(f, 1, shift)
	case tree.KeyHome:
		i.caretTo(f, i.lineEdge(f, false), shift)
	case tree.KeyEnd:
		i.caretTo(f, i.lineEdge(f, true), shift)
	case tree.KeyEnter:
		// A multiline field takes the newline; a single-line one reports the
		// value, which is what submitting a form means.
		if isMultiline(f) {
			i.insert(f, "\n")
		} else {
			f.Submit.Emit(f.Text.Get())
		}
	case tree.KeyEscape:
		if _, _, ok := f.Selection(); ok {
			f.ClearSelection()
			f.Base().MarkDirty(tree.DirtyPaint)
		}
	}
	i.markDirty()
}

// Char inserts committed text at the caret. IME commits arrive here as runes,
// so a composed run is inserted over the selection like typed text.
func (i *interactor) Char(r rune) {
	i.event("Char", "rune", string(r))
	f := i.focus
	if f == nil || r < 32 {
		return
	}
	i.insert(f, string(r))
}

// Preedit shows the IME composition in the focused field. It is not part of the
// value until the input method commits it.
func (i *interactor) Preedit(text string, caret int) {
	i.event("Preedit", "text", text, "caret", caret)
	f := i.focus
	if f == nil || !isTextField(f) {
		return
	}
	f.SetPreedit(text, caret)
	f.Base().MarkDirty(tree.DirtyPaint)
	i.markDirty()
}

// CommitText replaces the composition with finished text, over any selection.
func (i *interactor) CommitText(text string) {
	i.event("CommitText", "text", text)
	f := i.focus
	if f == nil || !isTextField(f) {
		return
	}
	f.ClearPreedit()
	f.Base().MarkDirty(tree.DirtyPaint)
	i.insert(f, text)
	i.imeSync()
}

// DeleteSurrounding removes text around the caret on the input method's
// request, which is how an IME corrects input it already committed.
func (i *interactor) DeleteSurrounding(before, after int) {
	i.event("DeleteSurrounding", "before", before, "after", after)
	f := i.focus
	if f == nil || !isTextField(f) {
		return
	}
	s := f.Text.Get()
	c := f.Caret
	a := clampRune(s, c-before)
	b := clampRune(s, c+after)
	i.deleteRange(f, a, b)
	i.imeSync()
}

// minInt returns the smaller of two ints.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isTextField reports whether the component takes IME input.
func isTextField(c *tree.Component) bool {
	n := c.Base().Node()
	return n != nil && (n.Kind() == "TextField" || n.Kind() == "SearchBar" || n.Kind() == "TextArea")
}

// imeSync tells the platform input method whether a field is focused and where
// its caret is, so composition and the candidate window follow the text.
func (i *interactor) imeSync() {
	d, ok := i.app.driver.(*gpuDriver)
	if !ok || d == nil || d.win == nil {
		return
	}
	f := i.focus
	if f == nil || !isTextField(f) {
		d.win.SetTextInput(false, 0, 0, 0, 0)
		return
	}
	n := i.focusNode(f)
	if n == nil {
		return
	}
	if isMultiline(f) {
		// A multiline field puts the candidate window on the caret's own line,
		// which may be scrolled out of the single-line box.
		box := paint.TextAreaBox(absoluteRect(n))
		lines := i.areaLines(f)
		lh := textAreaLineHeight()
		line, x := 0, 0.0
		for idx, ln := range lines {
			if f.Caret >= ln.Start && f.Caret <= ln.End {
				line = idx
				x = i.textWidth(ln.Text[:clampRune(ln.Text, f.Caret-ln.Start)])
				break
			}
		}
		d.win.SetTextInput(true, box.X+x, box.Y+float64(line)*lh-f.TextScroll, 2, lh)
		return
	}
	box := fieldTextBox(n)
	// The caret sits after the value up to the caret, plus any composition
	// text before its own caret.
	value := f.Text.Get()
	x := fieldTextStart(n) + i.textWidth(value[:minInt(f.Caret, len(value))])
	if f.Preedit != "" {
		x += i.textWidth(f.Preedit[:minInt(f.PreeditCaret, len(f.Preedit))])
	}
	y := box.Y + box.H/2 - 10
	d.win.SetTextInput(true, x, y, 2, 20)
}

func (i *interactor) Focus(focused bool) {
	i.event("Focus", "focused", focused)
	if !focused {
		i.setFocus(nil)
	}
}

func (i *interactor) Resize(width, height int, scale float32) {
	i.event("Resize", "width", width, "height", height, "scale", scale)
	if width <= 0 || height <= 0 {
		return
	}
	if d, ok := i.app.driver.(*gpuDriver); ok {
		d.pendingW, d.pendingH, d.pendingScale = width, height, scale
		d.resized = true
	}
	i.viewportW = float64(width) / float64(maxScale(scale))
	i.viewportH = float64(height) / float64(maxScale(scale))
	if i.tree.Root != nil {
		i.tree.Root.Base().MarkDirty(tree.DirtyLayout)
	}
	i.markDirty()
}

// Expose asks for a frame without touching the tree: the window system needs a
// buffer to finish an interactive resize, but nothing about the design changed.
func (i *interactor) Expose() {
	i.event("Expose")
	i.app.needsFrame = true
}

// ThemeChanged follows the desktop switching between light and dark: the tree
// re-reads the preference and a design that said `theme: app.systemMode` picks
// the other scheme on the next frame.
func (i *interactor) ThemeChanged() {
	i.event("ThemeChanged")
	if i.tree != nil {
		i.tree.SetSystemMode(systemMode())
	}
	i.app.needsFrame = true
}

// LocaleChanged follows the desktop's language setting: the tree re-reads it and
// a design that reads `app.systemLanguage` sees the new value on the next frame.
func (i *interactor) LocaleChanged() {
	i.event("LocaleChanged")
	if i.tree != nil {
		i.tree.SetSystemLanguage(systemLanguage())
	}
	i.app.needsFrame = true
}

func (i *interactor) Close() {
	i.event("Close")
	i.app.Quit()
}

func (i *interactor) markDirty() {
	// Every edit ends here, so this is the one place that keeps the input
	// method's caret in step with the field. The platform layer drops repeats.
	i.imeSync()
	i.app.needsFrame = true
	if i.app.driver != nil {
		i.app.driver.wake()
	}
}

// hit finds the topmost node at a point, respecting clipping and visibility.
func (i *interactor) hit(x, y float64) tree.Node {
	if i.tree.Root == nil {
		return nil
	}
	var found tree.Node
	walkHit(i.tree.Root.Base(), 0, 0, x, y, tree.Rect{
		X: 0, Y: 0, W: i.windowW(), H: i.windowH(),
	}, &found)
	return found
}

// walkHit visits children last-first so the topmost node wins.
func walkHit(b *tree.NodeBase, ox, oy float64, x, y float64, clip tree.Rect, found *tree.Node) {
	if !b.Visible() {
		return
	}
	br := b.Bounds()
	ox += br.X
	oy += br.Y
	box := tree.Rect{X: ox, Y: oy, W: br.W, H: br.H}

	inner := b.ContentBox(box)
	kidClip := clip
	if b.ClipsChildren() {
		kidClip = intersectRect(clip, inner)
	}
	ox2, oy2 := scrollShift(b)
	kids := b.Kids()
	for n := len(kids) - 1; n >= 0; n-- {
		kb := kids[n].Bounds()
		kb.X += ox2
		kb.Y += oy2
		walkHitOffset(kids[n], ox, oy, kb, x, y, kidClip, found)
		if *found != nil {
			return
		}
	}
	if box.Contains(x, y) && clip.Contains(x, y) {
		*found = b.Node()
	}
}

func walkHitOffset(b *tree.NodeBase, ox, oy float64, bounds tree.Rect, x, y float64, clip tree.Rect, found *tree.Node) {
	if !b.Visible() {
		return
	}
	ox += bounds.X
	oy += bounds.Y
	box := tree.Rect{X: ox, Y: oy, W: bounds.W, H: bounds.H}
	inner := b.ContentBox(box)
	kidClip := clip
	if b.ClipsChildren() {
		kidClip = intersectRect(clip, inner)
	}
	ox2, oy2 := scrollShift(b)
	kids := b.Kids()
	for n := len(kids) - 1; n >= 0; n-- {
		kb := kids[n].Bounds()
		kb.X += ox2
		kb.Y += oy2
		walkHitOffset(kids[n], ox, oy, kb, x, y, kidClip, found)
		if *found != nil {
			return
		}
	}
	if box.Contains(x, y) && clip.Contains(x, y) {
		*found = b.Node()
	}
}

func (i *interactor) windowW() float64 {
	if i.viewportW > 0 {
		return i.viewportW
	}
	if d, ok := i.app.driver.(*gpuDriver); ok {
		return d.windowLogicalW()
	}
	return 0
}

func (i *interactor) windowH() float64 {
	if i.viewportH > 0 {
		return i.viewportH
	}
	if d, ok := i.app.driver.(*gpuDriver); ok {
		return d.windowLogicalH()
	}
	return 0
}

// scrollShift is how much a node's children are moved by its scroll offset.
func scrollShift(b *tree.NodeBase) (x, y float64) {
	switch b.ScrollAxis() {
	case tree.AxisX:
		return -b.ScrollOffset(), 0
	case tree.AxisY:
		return 0, -b.ScrollOffset()
	}
	return 0, 0
}

// setFocus moves keyboard focus, clearing the old field.
func (i *interactor) setFocus(c *tree.Component) {
	if i.focus == c {
		return
	}
	if i.focus != nil {
		// Leaving a field ends its composition: the input method will not
		// commit text for a field the user has left.
		i.focus.ClearPreedit()
		i.focus.Focused = false
		i.focus.Base().MarkDirty(tree.DirtyPaint)
	}
	i.focus = c
	if c != nil {
		c.Focused = true
		c.Base().MarkDirty(tree.DirtyPaint)
	}
	i.imeSync()
	i.markDirty()
}

// focusables returns the nodes Tab can reach, in document order. Hidden
// subtrees and disabled controls are skipped, which is what makes focus order
// match what the user can see.
func (i *interactor) focusables() []tree.Node {
	var out []tree.Node
	var walk func(tree.Node)
	walk = func(n tree.Node) {
		if n == nil || !n.Base().Visible() {
			return
		}
		// Only real controls take focus. A plain node with a click handler is a
		// hit area or a scrim; ringing a whole app bar on click would be noise.
		c := comp(n)
		if c != nil && !c.Disabled.Get() && interactive(n) {
			out = append(out, n)
		}
		for _, k := range n.Base().Kids() {
			walk(k.Node())
		}
	}
	if i.tree != nil {
		walk(i.tree.Root)
	}
	return out
}

// setKbd moves keyboard focus. A text field also takes the character focus, so
// Tab lands in a field ready to type; any other target releases the field.
func (i *interactor) setKbd(n tree.Node) {
	if i.kbd == n {
		return
	}
	if old := i.kbd; old != nil {
		if c := comp(old); c != nil {
			c.Focused = false
			c.FocusRing = false
			c.Base().MarkDirty(tree.DirtyPaint)
		}
	}
	i.kbd = n
	if n == nil {
		return
	}
	c := comp(n)
	if c == nil {
		return
	}
	if isTextField(c) {
		i.setFocus(c)
	} else {
		i.setFocus(nil)
		c.Focused = true
		c.Base().MarkDirty(tree.DirtyPaint)
	}
	i.markDirty()
}

// tabFocus moves to the next or previous focusable and wraps at the ends.
func (i *interactor) tabFocus(back bool) {
	list := i.focusables()
	if len(list) == 0 {
		return
	}
	idx := -1
	for j, n := range list {
		if n == i.kbd {
			idx = j
			break
		}
	}
	next := 0
	if idx >= 0 {
		if back {
			next = (idx - 1 + len(list)) % len(list)
		} else {
			next = (idx + 1) % len(list)
		}
	}
	i.setKbd(list[next])
	// Tab is keyboard focus, so it is the one path that asks for the M3 ring.
	if c := comp(list[next]); c != nil {
		c.FocusRing = true
		c.Base().MarkDirty(tree.DirtyPaint)
	}
	i.markDirty()
}

// sliderStep is the arrow-key increment as a fraction of the range, matching
// M3's 5% suggestion when the slider declares no step of its own.
const sliderStep = 0.05

// sliderKey applies an arrow, Home or End press to a focused slider and reports
// whether it consumed the key. Shift makes an arrow jump ten steps.
func (i *interactor) sliderKey(n tree.Node, k tree.Key, mods tree.Mods) bool {
	c := comp(n)
	if c == nil {
		return false
	}
	lo, hi := c.Min.Get(), c.Max.Get()
	if hi <= lo {
		return false
	}
	step := c.Step.Get()
	if step <= 0 {
		step = (hi - lo) * sliderStep
	}
	if mods.Shift {
		step *= 10
	}
	v := c.Value.Get()
	switch k {
	case tree.KeyRight, tree.KeyUp:
		v += step
	case tree.KeyLeft, tree.KeyDown:
		v -= step
	case tree.KeyHome:
		v = lo
	case tree.KeyEnd:
		v = hi
	default:
		return false
	}
	// Snap to the grid so repeated steps do not drift off a declared step.
	if s := c.Step.Get(); s > 0 {
		v = lo + float64(int((v-lo)/s+0.5))*s
	}
	v = math.Max(lo, math.Min(hi, v))
	if v == c.Value.Get() {
		return true
	}
	c.Value.Set(v)
	c.Change.Emit(v)
	c.Base().MarkDirty(tree.DirtyPaint)
	i.markDirty()
	return true
}

// activateKey presses the focused node, the way a click would. Coordinates are
// the centre of the node in window space, because a click handler may read them.
func (i *interactor) activateKey(n tree.Node) {
	x, y, w, h := absoluteBounds(n)
	i.activate(n, comp(n), x+w/2, y+h/2)
}

// absoluteBounds sums the layout offsets up to the window.
func absoluteBounds(n tree.Node) (x, y, w, h float64) {
	b := n.Base().Bounds()
	x, y, w, h = b.X, b.Y, b.W, b.H
	for p := n.Base().Parent(); p != nil; p = p.Base().Parent() {
		pb := p.Base().Bounds()
		x += pb.X
		y += pb.Y
	}
	return x, y, w, h
}

// fieldAt finds the text field at or above a hit node.
func fieldAt(n tree.Node) *tree.Component {
	for n != nil {
		if k := n.Kind(); k == "TextField" || k == "SearchBar" || k == "TextArea" {
			return comp(n)
		}
		n = n.Base().Parent()
	}
	return nil
}

// selectableChildAt returns the nearest selectable child of a container hit.
func selectableChildAt(t *tree.Tree, hit tree.Node) tree.Node {
	for n := hit; n != nil; n = n.Base().Parent() {
		switch n.Kind() {
		case "Tab", "SegmentedButton", "MenuItem", "ListItem":
			return n
		}
	}
	return nil
}

func isDescendantOf(n, ancestor tree.Node) bool {
	for n != nil {
		if n == ancestor {
			return true
		}
		n = n.Base().Parent()
	}
	return false
}

// deleteRange removes s[a:b], leaving the caret at a. Ctrl+Backspace and
// Ctrl+Delete use it to delete a whole word at once.
func (i *interactor) deleteRange(f *tree.Component, a, b int) {
	s := f.Text.Get()
	a, b = clampRune(s, a), clampRune(s, b)
	if a >= b {
		return
	}
	ns := s[:a] + s[b:]
	f.Text.Set(ns)
	f.Caret = a
	f.ClearSelection()
	f.Base().MarkDirty(tree.DirtyPaint)
	f.Edit.Emit(ns)
}

func (i *interactor) deleteBefore(f *tree.Component) {
	s := f.Text.Get()
	c := f.Caret
	if c <= 0 || c > len(s) {
		return
	}
	p := prevRune(s, c)
	ns := s[:p] + s[c:]
	f.Text.Set(ns)
	f.Caret = p
	f.Base().MarkDirty(tree.DirtyPaint)
	f.Edit.Emit(ns)
}

func (i *interactor) deleteAfter(f *tree.Component) {
	s := f.Text.Get()
	c := f.Caret
	if c >= len(s) {
		return
	}
	n := nextRune(s, c)
	ns := s[:c] + s[n:]
	f.Text.Set(ns)
	f.Base().MarkDirty(tree.DirtyPaint)
	f.Edit.Emit(ns)
}

// drag updates a slider from the pointer position.
func (i *interactor) drag(x, y float64) {
	if i.pressed == nil || i.pressed.Kind() != "Slider" {
		return
	}
	c := comp(i.pressed)
	_ = y
	box := absoluteRect(i.pressed)
	inset := 2.0 // half the 4dp handle, matching paintSlider
	trackW := box.W - 2*inset
	if trackW <= 0 {
		return
	}
	frac := (x - box.X - inset) / trackW
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	lo, hi := c.Min.Get(), c.Max.Get()
	v := lo + frac*(hi-lo)
	if step := c.Step.Get(); step > 0 {
		v = lo + float64(int((v-lo)/step+0.5))*step
	}
	c.Value.Set(v)
	c.Change.Emit(v)
	i.markDirty()
}

// caretFromPoint places the caret at the nearest rune boundary to x, extending
// the selection when extend is set.
func (i *interactor) caretFromPoint(f *tree.Component, x, y float64, extend bool) {
	idx := i.indexAtXY(f, x, y)
	f.MoveCaret(idx, extend)
	f.Base().MarkDirty(tree.DirtyPaint)
	i.markDirty()
}

// indexAt maps an x position to the nearest byte index on the field's only line.
func (i *interactor) indexAt(f *tree.Component, x float64) int {
	return i.indexAtXY(f, x, 0)
}

// indexAtXY is the multiline form: a TextArea is mapped through its wrapped
// lines, everything else through the single line it draws.
func (i *interactor) indexAtXY(f *tree.Component, x, y float64) int {
	if isMultiline(f) {
		return i.areaIndexAt(f, x, y)
	}
	rel := x - fieldTextStart(i.focusNode(f))
	if rel <= 0 {
		return 0
	}
	s := f.Text.Get()
	best, bestD := 0, rel
	for idx := range s {
		if idx == 0 {
			continue
		}
		if d := math.Abs(i.textWidth(s[:idx]) - rel); d < bestD {
			best, bestD = idx, d
		}
	}
	if d := math.Abs(i.textWidth(s) - rel); d < bestD {
		best = len(s)
	}
	return best
}

// fieldTextStart is where the visible value text begins inside a field,
// matching the painter: a 16dp inset plus room for a leading icon, minus the
// horizontal scroll that keeps the caret in view. A search bar starts after its
// own 52dp search icon instead.
func fieldTextStart(n tree.Node) float64 {
	c := comp(n)
	if c == nil {
		return absoluteRect(n).X + 16
	}
	// The text may be scrolled left to keep the caret in view.
	return fieldTextBox(n).X - c.TextScroll
}

// fieldTextBox is the visible area the value text occupies, in window
// coordinates. It matches the painter: 16dp inset plus room for icons, or the
// 52dp search inset.
func fieldTextBox(n tree.Node) tree.Rect {
	box := absoluteRect(n)
	if n.Kind() == "SearchBar" {
		return tree.Rect{X: box.X + 52, Y: box.Y, W: box.W - 104, H: box.H}
	}
	x, w := box.X+16, box.W-32
	if c := comp(n); c != nil {
		if c.Icon.Get() != "" {
			x += 40
			w -= 40
		}
		if c.Trailing.Get() != "" {
			w -= 40
		}
	}
	return tree.Rect{X: x, Y: box.Y, W: w, H: box.H}
}

// textWidth is the advance width of field text, measured the same way the
// painter measures it so carets land between the glyphs they follow.
func (i *interactor) textWidth(s string) float64 {
	if s == "" {
		return 0
	}
	if i.measure != nil {
		return i.measure(s, 16, 400)
	}
	// shortcut: without a font library (headless), the painter's 0.55 estimate
	// stands in for shaped advances.
	return float64(len([]rune(s))) * 16 * 0.55
}

// lineEdge is the start or end of the logical line the caret is on, which is
// what Home and End mean in a multiline field.
func (i *interactor) lineEdge(f *tree.Component, end bool) int {
	s, c := f.Text.Get(), clampRune(f.Text.Get(), f.Caret)
	if !isMultiline(f) {
		if end {
			return len(s)
		}
		return 0
	}
	if end {
		if n := strings.IndexByte(s[c:], '\n'); n >= 0 {
			return c + n
		}
		return len(s)
	}
	if n := strings.LastIndexByte(s[:c], '\n'); n >= 0 {
		return n + 1
	}
	return 0
}

// moveCaretVertical walks the wrapped lines, keeping the caret at the same
// horizontal position rather than the same byte column, which is what the eye
// expects when a line wraps.
func (i *interactor) moveCaretVertical(f *tree.Component, dir int, extend bool) {
	if !isMultiline(f) {
		// Single-line fields treat Up/Down as home/end, as they always have.
		if dir < 0 {
			i.caretTo(f, 0, extend)
		} else {
			i.caretTo(f, len(f.Text.Get()), extend)
		}
		return
	}
	lines := i.areaLines(f)
	if len(lines) == 0 {
		return
	}
	cur, x := 0, 0.0
	for idx, ln := range lines {
		if f.Caret >= ln.Start && f.Caret <= ln.End {
			cur = idx
			x = i.textWidth(ln.Text[:clampRune(ln.Text, f.Caret-ln.Start)])
			break
		}
	}
	next := cur + dir
	if next < 0 || next >= len(lines) {
		return
	}
	target := lines[next]
	best, bestD := target.Start, x
	for idx := range target.Text {
		if idx == 0 {
			continue
		}
		if d := math.Abs(i.textWidth(target.Text[:idx]) - x); d < bestD {
			best, bestD = target.Start+idx, d
		}
	}
	if d := math.Abs(i.textWidth(target.Text) - x); d < bestD {
		best = target.End
	}
	i.caretTo(f, best, extend)
}

// areaLines wraps the focused TextArea's value the way the painter does, so the
// caret and the pointer agree on which visual line is which.
func (i *interactor) areaLines(f *tree.Component) []text.Line {
	n := i.focusNode(f)
	if n == nil {
		return nil
	}
	box := paint.TextAreaBox(absoluteRect(n))
	return paint.TextAreaLines(f.Text.Get(), box.W, i.textWidth)
}

// areaIndexAt maps a pointer to a byte index inside a wrapped TextArea.
func (i *interactor) areaIndexAt(f *tree.Component, x, y float64) int {
	n := i.focusNode(f)
	if n == nil {
		return 0
	}
	box := paint.TextAreaBox(absoluteRect(n))
	lines := i.areaLines(f)
	if len(lines) == 0 {
		return 0
	}
	lh := textAreaLineHeight()
	li := int((y - box.Y + f.TextScroll) / lh)
	if li < 0 {
		li = 0
	}
	if li >= len(lines) {
		li = len(lines) - 1
	}
	line := lines[li]
	rel := x - box.X
	if rel <= 0 {
		return line.Start
	}
	best, bestD := line.Start, rel
	for idx := range line.Text {
		if idx == 0 {
			continue
		}
		if d := math.Abs(i.textWidth(line.Text[:idx]) - rel); d < bestD {
			best, bestD = line.Start+idx, d
		}
	}
	if d := math.Abs(i.textWidth(line.Text) - rel); d < bestD {
		best = line.End
	}
	return best
}

// textAreaLineHeight is the line advance the TextArea painter uses.
func textAreaLineHeight() float64 {
	lh := m3.RoleStyle("bodyLarge").LineHeight
	if lh <= 0 {
		lh = 16 * 1.2
	}
	return lh
}

// isMultiline reports whether a field holds more than one line.
func isMultiline(c *tree.Component) bool {
	n := c.Base().Node()
	return n != nil && n.Kind() == "TextArea"
}

// pressText handles a press inside a field: single click places the caret,
// shift-click extends, double click selects a word, triple click selects all.
func (i *interactor) pressText(f *tree.Component, x, y float64, shift bool) {
	idx := i.indexAtXY(f, x, y)
	now := time.Now()
	multi := f == i.lastField && now.Sub(i.lastClick) < 400*time.Millisecond &&
		math.Abs(x-i.lastClickX) <= 6 && math.Abs(y-i.lastClickY) <= 6
	if multi {
		i.clickCount++
	} else {
		i.clickCount = 1
	}
	i.lastClick, i.lastClickX, i.lastClickY, i.lastField = now, x, y, f

	switch {
	case multi && i.clickCount >= 3:
		f.SelectAll()
	case multi:
		a, b := wordBounds(f.Text.Get(), idx)
		f.SelAnchor, f.Caret = a, b
	case shift:
		f.MoveCaret(idx, true)
	default:
		f.MoveCaret(idx, false)
	}
	f.Base().MarkDirty(tree.DirtyPaint)
	i.markDirty()
}

// insert replaces the selection (or the caret position) with text.
func (i *interactor) insert(f *tree.Component, text string) {
	if text == "" {
		return
	}
	s := f.Text.Get()
	a := clampRune(s, f.Caret)
	b := a
	if x, y, ok := f.Selection(); ok {
		a, b = x, y
	}
	f.Text.Set(s[:a] + text + s[b:])
	f.Caret = a + len(text)
	f.ClearSelection()
	f.Base().MarkDirty(tree.DirtyPaint)
	f.Edit.Emit(f.Text.Get())
	i.markDirty()
}

// deleteSelection removes the selected range and reports whether there was one.
func (i *interactor) deleteSelection(f *tree.Component) bool {
	a, b, ok := f.Selection()
	if !ok {
		return false
	}
	s := f.Text.Get()
	f.Text.Set(s[:a] + s[b:])
	f.Caret = a
	f.ClearSelection()
	f.Base().MarkDirty(tree.DirtyPaint)
	f.Edit.Emit(f.Text.Get())
	i.markDirty()
	return true
}

// copySelection puts the selected text on the clipboard.
func (i *interactor) copySelection(f *tree.Component) bool {
	a, b, ok := f.Selection()
	if !ok {
		return false
	}
	i.putClipboard(f.Text.Get()[a:b])
	return true
}

func (i *interactor) clipboardText() string {
	if i.getClipboard != nil {
		return i.getClipboard()
	}
	return platform.Clipboard()
}

func (i *interactor) putClipboard(s string) {
	if s == "" {
		return
	}
	if i.setClipboard != nil {
		i.setClipboard(s)
		return
	}
	platform.SetClipboard(s)
}

// singleLine flattens pasted text: a TextField is one line, so newlines become
// spaces instead of silently drawing outside the box.
func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

// moveCaret moves the caret by one rune or one word. Without shift, an existing
// selection collapses to the edge the movement points at.
func (i *interactor) moveCaret(f *tree.Component, dir int, extend, word bool) {
	s := f.Text.Get()
	if !extend {
		if a, b, ok := f.Selection(); ok {
			to := a
			if dir > 0 {
				to = b
			}
			f.MoveCaret(to, false)
			f.Base().MarkDirty(tree.DirtyPaint)
			return
		}
	}
	to := f.Caret
	switch {
	case dir < 0 && word:
		to = prevWord(s, to)
	case dir < 0:
		to = prevRune(s, to)
	case word:
		to = nextWord(s, to)
	default:
		to = nextRune(s, to)
	}
	f.MoveCaret(to, extend)
	f.Base().MarkDirty(tree.DirtyPaint)
}

// caretTo moves the caret to an absolute index, extending when shift is held.
func (i *interactor) caretTo(f *tree.Component, to int, extend bool) {
	f.MoveCaret(to, extend)
	f.Base().MarkDirty(tree.DirtyPaint)
}

// wordBounds expands idx to the run of runes of the same class (space, word or
// punctuation), which is what a double click selects.
func wordBounds(s string, idx int) (int, int) {
	idx = clampRune(s, idx)
	if idx == len(s) {
		if len(s) == 0 {
			return 0, 0
		}
		_, size := utf8.DecodeLastRuneInString(s)
		idx -= size
	}
	r, _ := utf8.DecodeRuneInString(s[idx:])
	cl := runeClass(r)
	start, end := idx, idx
	for start > 0 {
		p, size := utf8.DecodeLastRuneInString(s[:start])
		if runeClass(p) != cl {
			break
		}
		start -= size
	}
	for end < len(s) {
		q, size := utf8.DecodeRuneInString(s[end:])
		if runeClass(q) != cl {
			break
		}
		end += size
	}
	return start, end
}

func prevWord(s string, c int) int {
	c = clampRune(s, c)
	for c > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:c])
		if !unicode.IsSpace(r) {
			break
		}
		c -= size
	}
	if c == 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(s[:c])
	cl := runeClass(r)
	for c > 0 {
		p, size := utf8.DecodeLastRuneInString(s[:c])
		if runeClass(p) != cl {
			break
		}
		c -= size
	}
	return c
}

func nextWord(s string, c int) int {
	c = clampRune(s, c)
	for c < len(s) {
		r, size := utf8.DecodeRuneInString(s[c:])
		if !unicode.IsSpace(r) {
			break
		}
		c += size
	}
	if c == len(s) {
		return c
	}
	r, _ := utf8.DecodeRuneInString(s[c:])
	cl := runeClass(r)
	for c < len(s) {
		q, size := utf8.DecodeRuneInString(s[c:])
		if runeClass(q) != cl {
			break
		}
		c += size
	}
	return c
}

// runeClass groups runes for word selection: 0 space, 1 word, 2 punctuation.
// nextWordEnd is where Ctrl+Delete stops: the end of the next word plus the
// spaces after it, so repeated presses remove one word and its separator.
// Ctrl+Right keeps using nextWord, which stops at the word end.
func nextWordEnd(s string, c int) int {
	c = nextWord(s, c)
	for c < len(s) {
		r, size := utf8.DecodeRuneInString(s[c:])
		if !unicode.IsSpace(r) {
			break
		}
		c += size
	}
	return c
}

func runeClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 1
	}
	return 2
}

// clampRune clamps a byte index and moves it back to a rune boundary.
func clampRune(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i >= len(s) {
		return len(s)
	}
	for i > 0 && s[i]&0xC0 == 0x80 {
		i--
	}
	return i
}

// focusNode returns the tree node wrapping a component.
func (i *interactor) focusNode(f *tree.Component) tree.Node {
	var found tree.Node
	i.tree.Walk(func(n tree.Node) {
		if found == nil && comp(n) == f {
			found = n
		}
	})
	return found
}

// absoluteRect returns a node's rectangle in window coordinates.
func absoluteRect(n tree.Node) tree.Rect {
	if n == nil {
		return tree.Rect{}
	}
	b := n.Base().Bounds()
	x, y := b.X, b.Y
	for p := n.Base().Parent(); p != nil; p = p.Base().Parent() {
		pb := p.Base().Bounds()
		x += pb.X
		y += pb.Y
		switch p.Base().ScrollAxis() {
		case tree.AxisX:
			x -= p.Base().ScrollOffset()
		case tree.AxisY:
			y -= p.Base().ScrollOffset()
		}
	}
	return tree.Rect{X: x, Y: y, W: b.W, H: b.H}
}

// hitRect reports whether a point falls in a box.
func hitRect(r tree.Rect, x, y float64) bool {
	return x >= r.X && x < r.Right() && y >= r.Y && y < r.Bottom()
}

func intersectRect(a, b tree.Rect) tree.Rect {
	x0 := maxf(a.X, b.X)
	y0 := maxf(a.Y, b.Y)
	x1 := minf(a.Right(), b.Right())
	y1 := minf(a.Bottom(), b.Bottom())
	if x1 < x0 {
		x1 = x0
	}
	if y1 < y0 {
		y1 = y0
	}
	return tree.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func prevRune(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(s) {
		i = len(s)
	}
	for j := i - 1; j > 0 && s[j]&0xC0 == 0x80; j-- {
		i = j
	}
	return i - 1
}

func nextRune(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	i++
	for i < len(s) && s[i]&0xC0 == 0x80 {
		i++
	}
	return i
}

func toTreeKey(k platform.Key) tree.Key {
	switch k {
	case platform.KeyBackspace:
		return tree.KeyBackspace
	case platform.KeyDelete:
		return tree.KeyDelete
	case platform.KeyLeft:
		return tree.KeyLeft
	case platform.KeyRight:
		return tree.KeyRight
	case platform.KeyHome:
		return tree.KeyHome
	case platform.KeyEnd:
		return tree.KeyEnd
	case platform.KeyEnter:
		return tree.KeyEnter
	case platform.KeyTab:
		return tree.KeyTab
	case platform.KeyEscape:
		return tree.KeyEscape
	case platform.KeyUp:
		return tree.KeyUp
	case platform.KeyDown:
		return tree.KeyDown
	case platform.KeyA:
		return tree.KeyA
	case platform.KeyC:
		return tree.KeyC
	case platform.KeyV:
		return tree.KeyV
	case platform.KeyX:
		return tree.KeyX
	}
	return tree.KeyNone
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
