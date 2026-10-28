package ui

import (
	"github.com/ksckaan1/goui/internal/platform"
	"github.com/ksckaan1/goui/internal/tree"
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

	viewportW, viewportH float64
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
		"SearchBar", "ListItem", "MenuItem", "Tab", "Card":
		return true
	}
	return false
}

func (i *interactor) MouseMove(x, y float64) {
	hit := i.hit(x, y)
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

func (i *interactor) MouseDown(x, y float64, button int) {
	hit := i.hit(x, y)
	i.pressX, i.pressY = x, y
	i.pressed = nil
	i.dragging = false

	// Text fields take focus wherever the press lands inside them.
	if f := fieldAt(hit); f != nil {
		i.setFocus(f)
		i.caretFromPoint(f, x)
		return
	}
	i.setFocus(nil)

	if hit == nil {
		return
	}
	if c := comp(hit); c != nil && !c.Disabled.Get() && interactive(hit) {
		i.pressed = hit
		c.Pressed = true
		c.Base().MarkDirty(tree.DirtyPaint)
		if hit.Kind() == "Slider" {
			i.dragging = true
			i.drag(x, y)
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
	}()
	if i.pressed == nil {
		i.dragging = false
		return
	}
	target := i.pressed
	c := comp(target)
	if c == nil {
		return
	}
	if c.Pressed {
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
	case "Tab", "SegmentedButton", "MenuItem", "ListItem":
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
	}
	c.Click.Emit(tree.ClickEvent{X: x, Y: y})
	i.markDirty()
}

func (i *interactor) Scroll(dx, dy float64) {
	hit := i.hit(i.lastX(), i.lastY())
	for n := hit; n != nil; n = n.Base().Parent() {
		if scrollable(n) {
			c := comp(n)
			c.ScrollY -= dy * 48
			if c.ScrollY < 0 {
				c.ScrollY = 0
			}
			n.Base().MarkDirty(tree.DirtyPaint)
			i.markDirty()
			return
		}
	}
}

func scrollable(n tree.Node) bool {
	switch n.Kind() {
	case "List", "Menu", "NavigationDrawer", "Dialog", "DatePicker":
		return true
	}
	return false
}

func (i *interactor) Key(k platform.Key, mods platform.Mods) {
	i.key(toTreeKey(k), tree.Mods{Shift: mods.Shift, Ctrl: mods.Ctrl, Alt: mods.Alt, Super: mods.Super})
}

// key handles a key press in framework terms, so tests need no platform types.
func (i *interactor) key(k tree.Key, mods tree.Mods) {
	f := i.focus
	if f == nil {
		return
	}
	switch k {
	case tree.KeyBackspace:
		i.deleteBefore(f)
	case tree.KeyDelete:
		i.deleteAfter(f)
	case tree.KeyLeft:
		f.Caret = prevRune(f.Text.Get(), f.Caret)
		f.Base().MarkDirty(tree.DirtyPaint)
	case tree.KeyRight:
		f.Caret = nextRune(f.Text.Get(), f.Caret)
		f.Base().MarkDirty(tree.DirtyPaint)
	case tree.KeyHome:
		f.Caret = 0
		f.Base().MarkDirty(tree.DirtyPaint)
	case tree.KeyEnd:
		f.Caret = len(f.Text.Get())
		f.Base().MarkDirty(tree.DirtyPaint)
	case tree.KeyEnter:
		f.Submit.Emit(f.Text.Get())
	}
	if mods.Control() && (k == tree.KeyLeft || k == tree.KeyRight) {
		// Word-wise movement is not implemented; keep the caret valid.
	}
	i.markDirty()
}

func (i *interactor) Char(r rune) {
	f := i.focus
	if f == nil || r < 32 {
		return
	}
	s := f.Text.Get()
	c := f.Caret
	if c > len(s) {
		c = len(s)
	}
	ns := s[:c] + string(r) + s[c:]
	f.Text.Set(ns)
	f.Caret = c + len(string(r))
	f.Base().MarkDirty(tree.DirtyPaint)
	f.Edit.Emit(ns)
	i.markDirty()
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

	inner := tree.Rect{
		X: box.X + b.Padding() + b.BorderWidth(),
		Y: box.Y + b.Padding() + b.BorderWidth(),
		W: box.W - 2*(b.Padding()+b.BorderWidth()),
		H: box.H - 2*(b.Padding()+b.BorderWidth()),
	}
	kidClip := intersectRect(clip, inner)
	oy2 := 0.0
	if c := comp(b.Node()); c != nil && scrollable(b.Node()) {
		oy2 = -c.ScrollY
	}
	kids := b.Kids()
	for n := len(kids) - 1; n >= 0; n-- {
		kb := kids[n].Bounds()
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
	inner := tree.Rect{
		X: box.X + b.Padding() + b.BorderWidth(),
		Y: box.Y + b.Padding() + b.BorderWidth(),
		W: box.W - 2*(b.Padding()+b.BorderWidth()),
		H: box.H - 2*(b.Padding()+b.BorderWidth()),
	}
	kidClip := intersectRect(clip, inner)
	oy2 := 0.0
	if c := comp(b.Node()); c != nil && scrollable(b.Node()) {
		oy2 = -c.ScrollY
	}
	kids := b.Kids()
	for n := len(kids) - 1; n >= 0; n-- {
		kb := kids[n].Bounds()
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

func (i *interactor) lastX() float64 { return i.pressX }
func (i *interactor) lastY() float64 { return i.pressY }

// setFocus moves keyboard focus, clearing the old field.
func (i *interactor) setFocus(c *tree.Component) {
	if i.focus == c {
		return
	}
	if i.focus != nil {
		i.focus.Focused = false
		i.focus.Base().MarkDirty(tree.DirtyPaint)
	}
	i.focus = c
	if c != nil {
		c.Focused = true
		c.Base().MarkDirty(tree.DirtyPaint)
	}
	i.markDirty()
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
	inset := 10.0
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

// caretFromPoint places the caret at the nearest rune boundary to x.
func (i *interactor) caretFromPoint(f *tree.Component, x float64) {
	box := absoluteRect(i.focusNode(f))
	rel := x - box.X - 16
	if rel < 0 {
		rel = 0
	}
	s := f.Text.Get()
	// shortcut: caret placement assumes 0.55*fontSize per character instead of
	// shaped advances. Upgrade when text selection lands.
	per := l_estimateCharWidth(f)
	idx := int(rel / per)
	if idx > len(s) {
		idx = len(s)
	}
	for idx > 0 && idx < len(s) && s[idx]&0xC0 == 0x80 {
		idx--
	}
	f.Caret = idx
	f.Base().MarkDirty(tree.DirtyPaint)
}

func l_estimateCharWidth(f *tree.Component) float64 {
	// BodyLarge is 16sp.
	return 16 * 0.55
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
