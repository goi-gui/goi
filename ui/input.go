package ui

import (
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ksckaan1/goi/internal/platform"
	"github.com/ksckaan1/goi/internal/tree"
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
	scrollNode tree.Node
	scrollFrom float64
	dragFromX  float64
	dragFromY  float64
	scrolled   bool

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
	dragX      float64
	clickCount int
	lastClick  time.Time
	lastClickX float64
	lastClickY float64
	lastField  *tree.Component

	// Clipboard hooks; nil uses the platform clipboard.
	getClipboard func() string
	setClipboard func(string)

	// measure shapes a string for caret and selection geometry. The driver sets
	// it to the same measurer the painter uses; nil falls back to an estimate.
	measure func(s string, size float64, weight int) float64

	// setCursor changes the native pointer shape; nil in headless tests.
	setCursor func(platform.Cursor)

	viewportW, viewportH float64

	// textSel is the selectable Text the pointer last selected, so Ctrl+C can
	// copy it after the pointer is released.
	textSel *tree.Text

	// Fling keeps a wheel or touchpad scroll moving after the gesture ends.
	flingNode tree.Node
	flingV    float64 // logical pixels per second
	lastWheel time.Time
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
		"SearchBar", "ListItem", "MenuItem", "Tab", "Card", "NavigationItem":
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

func (i *interactor) MouseMove(x, y float64) {
	i.cursorX, i.cursorY = x, y
	if i.selecting && i.textSel != nil {
		i.pointer(platform.CursorText)
		i.textSel.SelB = i.textIndexAt(i.textSel, x, y)
		i.textSel.Base().MarkDirty(tree.DirtyPaint)
		i.markDirty()
		return
	}
	hit := i.hit(x, y)
	i.pointerAt(hit)
	if i.selecting && i.textField != nil {
		i.dragX = x
		i.caretFromPoint(i.textField, x, true)
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
	for _, n := range i.tree.All() {
		c := comp(n)
		if c == nil {
			continue
		}
		want := isDescendantOf(hit, n) && interactive(n)
		if c.Hover != want {
			c.Hover = want
			c.Base().MarkDirty(tree.DirtyPaint)
			changed = true
		}
	}
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
	if selectableTextAt(hit) != nil {
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
	i.scrollNode = nil
	field := fieldAt(hit)
	// A press in a scroll area can become a drag; a slider keeps the pointer,
	// and so does a text field, where the drag selects text.
	if hit != nil && hit.Kind() != "Slider" && field == nil {
		i.scrollNode = scrollableAt(hit)
		if i.scrollNode != nil {
			i.scrollFrom = i.scrollNode.Base().ScrollOffset()
		}
	}

	// A selectable Text starts a selection drag before any container click:
	// reading a paragraph should not also press the card around it. The press
	// consumes the gesture, so MouseUp has nothing to activate.
	if t := selectableTextAt(hit); t != nil && button == 0 {
		i.setFocus(nil)
		i.setKbd(nil)
		i.scrollNode = nil
		idx := i.textIndexAt(t, x, y)
		t.SelA, t.SelB = idx, idx
		i.textSel = t
		i.selecting = true
		t.Base().MarkDirty(tree.DirtyPaint)
		i.markDirty()
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
	if sel := selectableAt(i.tree, hit); sel != nil {
		i.pressed = sel
	}
	_ = button
}

func (i *interactor) MouseUp(x, y float64, button int) {
	defer func() {
		i.pressed = nil
		i.dragging = false
		i.scrollNode = nil
		i.scrolled = false
		i.textField = nil
		i.selecting = false
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
	}
	c.Click.Emit(tree.ClickEvent{X: x, Y: y})
	i.markDirty()
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

// Scroll turns wheel and touchpad input into a scroll offset on the nearest
// scrollable ancestor of the pointer.
func (i *interactor) Scroll(dx, dy float64) {
	n := scrollableAt(i.hit(i.cursorX, i.cursorY))
	if n == nil {
		return
	}
	b := n.Base()
	delta := dy
	if b.ScrollAxis() == tree.AxisX {
		// Most mice only have a vertical wheel; use it for a horizontal area.
		if dx == 0 {
			dx = dy
		}
		delta = dx
	}
	moved := -delta * 48
	b.SetScrollOffset(b.ScrollOffset() + moved)
	b.MarkDirty(tree.DirtyPaint)
	i.startFling(n, moved)
	// The content moved under a stationary pointer, so the shape may be stale.
	i.pointerAt(i.hit(i.cursorX, i.cursorY))
	i.markDirty()
}

// startFling turns the last wheel step into a velocity, so a fast flick coasts.
// A step that arrives after a pause starts a new gesture instead of adding to a
// stale one.
func (i *interactor) startFling(n tree.Node, moved float64) {
	now := time.Now()
	dt := now.Sub(i.lastWheel).Seconds()
	i.lastWheel = now
	if dt <= 0 || dt > 0.25 {
		i.flingNode = n
		i.flingV = moved / 0.05 // one step is about a 20 Hz gesture
		return
	}
	i.flingNode = n
	i.flingV = moved / dt
	// A trackpad sends many tiny steps; cap what one tick can carry.
	if i.flingV > 8000 {
		i.flingV = 8000
	}
	if i.flingV < -8000 {
		i.flingV = -8000
	}
}

// flingFriction is the exponential decay per second. 6 is a short M3-like
// coast: about 400ms to a stop from a fast flick.
const flingFriction = 6.0

// tickFling advances a coasting scroll and reports whether the loop must keep
// drawing. It stops at the end of the content instead of bouncing.
func (i *interactor) tickFling(dt float64) bool {
	if i.flingNode == nil || i.flingV == 0 {
		return false
	}
	b := i.flingNode.Base()
	before := b.ScrollOffset()
	b.SetScrollOffset(before + i.flingV*dt)
	i.flingV *= math.Exp(-flingFriction * dt)
	after := b.ScrollOffset()
	// Hitting an edge kills the momentum; otherwise the offset would keep
	// asking for frames while nothing moves.
	if after == before || math.Abs(i.flingV) < 8 {
		i.flingNode = nil
		i.flingV = 0
		return false
	}
	b.MarkDirty(tree.DirtyPaint)
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
	delta := y - i.dragFromY
	if n.Base().ScrollAxis() == tree.AxisX {
		delta = x - i.dragFromX
	}
	if !i.scrolled {
		if math.Abs(delta) < 4 {
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
	n.Base().SetScrollOffset(i.scrollFrom - delta)
	n.Base().MarkDirty(tree.DirtyPaint)
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

func (i *interactor) Key(k platform.Key, mods platform.Mods) {
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
			i.insert(f, singleLine(i.clipboardText()))
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
	case tree.KeyHome, tree.KeyUp:
		i.caretTo(f, 0, shift)
	case tree.KeyEnd, tree.KeyDown:
		i.caretTo(f, len(f.Text.Get()), shift)
	case tree.KeyEnter:
		f.Submit.Emit(f.Text.Get())
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
	f := i.focus
	if f == nil || r < 32 {
		return
	}
	i.insert(f, string(r))
}

// Preedit shows the IME composition in the focused field. It is not part of the
// value until the input method commits it.
func (i *interactor) Preedit(text string, caret int) {
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
	return n != nil && (n.Kind() == "TextField" || n.Kind() == "SearchBar")
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
	if !focused {
		i.setFocus(nil)
	}
}

func (i *interactor) Resize(width, height int, scale float32) {
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

func (i *interactor) Close() { i.app.Quit() }

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
		if k := n.Kind(); k == "TextField" || k == "SearchBar" {
			return comp(n)
		}
		n = n.Base().Parent()
	}
	return nil
}

// selectableAt returns the nearest selectable child of a container hit.
func selectableAt(t *tree.Tree, hit tree.Node) tree.Node {
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
func (i *interactor) caretFromPoint(f *tree.Component, x float64, extend bool) {
	idx := i.indexAt(f, x)
	f.MoveCaret(idx, extend)
	f.Base().MarkDirty(tree.DirtyPaint)
	i.markDirty()
}

// indexAt maps an x position to the nearest byte index inside the field text.
func (i *interactor) indexAt(f *tree.Component, x float64) int {
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

// pressText handles a press inside a field: single click places the caret,
// shift-click extends, double click selects a word, triple click selects all.
func (i *interactor) pressText(f *tree.Component, x, y float64, shift bool) {
	idx := i.indexAt(f, x)
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
	for _, n := range i.tree.All() {
		if comp(n) == f {
			return n
		}
	}
	return nil
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
