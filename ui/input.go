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

	// hover is the node under the pointer last frame.
	hover tree.Node
	// pressed is the node where the primary button went down.
	pressed tree.Node
	focus   *tree.TextInput

	dragging bool
}

func (i *interactor) MouseMove(x, y float64) {
	hit := i.hit(x, y)
	if hit == i.hover {
		return
	}
	i.hover = hit
	for _, n := range i.tree.All() {
		if b, ok := n.(*tree.Button); ok {
			b.SetHover(n == hit || isDescendantOf(hit, b))
		}
	}
	i.markDirty()
}

func (i *interactor) MouseDown(x, y float64, button int) {
	hit := i.hit(x, y)
	i.pressed = hit

	if ti := textInputAt(hit); ti != nil {
		i.setFocus(ti)
		i.caretFromPoint(ti, x)
		return
	}
	i.setFocus(nil)
	_ = button
}

func (i *interactor) MouseUp(x, y float64, button int) {
	defer func() { i.pressed = nil }()
	if i.pressed == nil {
		return
	}
	hit := i.hit(x, y)
	if hit != i.pressed {
		return // press and release must be on the same node
	}
	if btn := buttonAt(hit); btn != nil {
		btn.Click.Emit(tree.ClickEvent{X: x, Y: y, Button: button})
	}
}

func (i *interactor) Scroll(dx, dy float64) {
	_ = dx
	_ = dy
	// Scroll containers arrive in v1.1; nothing to do yet.
}

func (i *interactor) Key(k platform.Key, mods platform.Mods) {
	ti := i.focus
	if ti == nil {
		return
	}
	switch toTreeKey(k) {
	case tree.KeyBackspace:
		i.deleteBefore(ti)
	case tree.KeyDelete:
		i.deleteAfter(ti)
	case tree.KeyLeft:
		ti.SetCaret(prevRune(ti.Text.Get(), ti.Caret()))
	case tree.KeyRight:
		ti.SetCaret(nextRune(ti.Text.Get(), ti.Caret()))
	case tree.KeyHome:
		ti.SetCaret(0)
	case tree.KeyEnd:
		ti.SetCaret(len(ti.Text.Get()))
	case tree.KeyEnter:
		ti.Submit.Emit(ti.Text.Get())
	}
	_ = mods
	i.markDirty()
}

func (i *interactor) Char(r rune) {
	ti := i.focus
	if ti == nil || r < 32 {
		return
	}
	s := ti.Text.Get()
	c := ti.Caret()
	ns := s[:c] + string(r) + s[c:]
	ti.Text.Set(ns)
	ti.SetCaret(c + len(string(r)))
	ti.Change.Emit(ns)
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
	if i.tree.Root != nil {
		i.tree.Root.Base().MarkDirty(tree.DirtyLayout)
	}
	i.markDirty()
}

func (i *interactor) Close() {
	i.app.Quit()
}

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
		X: 0, Y: 0,
		W: i.windowW(), H: i.windowH(),
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
	kids := b.Kids()
	for n := len(kids) - 1; n >= 0; n-- {
		walkHit(kids[n], ox, oy, x, y, intersectRect(clip, inner), found)
		if *found != nil {
			return
		}
	}
	if box.Contains(x, y) && clip.Contains(x, y) {
		*found = b.Node()
	}
}

func (i *interactor) windowW() float64 {
	if d, ok := i.app.driver.(*gpuDriver); ok {
		return d.windowLogicalW()
	}
	return 0
}

func (i *interactor) windowH() float64 {
	if d, ok := i.app.driver.(*gpuDriver); ok {
		return d.windowLogicalH()
	}
	return 0
}

func (d *gpuDriver) windowLogicalW() float64 {
	return float64(d.pendingW) / float64(maxScale(d.pendingScale))
}

func (d *gpuDriver) windowLogicalH() float64 {
	return float64(d.pendingH) / float64(maxScale(d.pendingScale))
}

// setFocus moves keyboard focus, clearing the old field's repair state.
func (i *interactor) setFocus(ti *tree.TextInput) {
	if i.focus == ti {
		return
	}
	i.focus = ti
	i.markDirty()
}

// textInputAt returns the TextInput at a hit node or one of its ancestors.
func textInputAt(n tree.Node) *tree.TextInput {
	for n != nil {
		if ti, ok := n.(*tree.TextInput); ok {
			return ti
		}
		n = n.Base().Parent()
	}
	return nil
}

// buttonAt returns the nearest Button ancestor of a hit node.
func buttonAt(n tree.Node) *tree.Button {
	for n != nil {
		if b, ok := n.(*tree.Button); ok {
			return b
		}
		n = n.Base().Parent()
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

func (i *interactor) deleteBefore(ti *tree.TextInput) {
	s := ti.Text.Get()
	c := ti.Caret()
	if c == 0 {
		return
	}
	p := prevRune(s, c)
	ns := s[:p] + s[c:]
	ti.Text.Set(ns)
	ti.SetCaret(p)
	ti.Change.Emit(ns)
}

func (i *interactor) deleteAfter(ti *tree.TextInput) {
	s := ti.Text.Get()
	c := ti.Caret()
	if c >= len(s) {
		return
	}
	n := nextRune(s, c)
	ns := s[:c] + s[n:]
	ti.Text.Set(ns)
	ti.Change.Emit(ns)
}

// caretFromPoint places the caret at the nearest rune boundary to x.
func (i *interactor) caretFromPoint(ti *tree.TextInput, x float64) {
	b := ti.Base().Bounds()
	ox := absoluteX(ti)
	rel := x - ox - b.X
	// shortcut: 8px per byte; the text package will provide real hit testing.
	idx := int(rel / 8)
	s := ti.Text.Get()
	if idx < 0 {
		idx = 0
	}
	if idx > len(s) {
		idx = len(s)
	}
	ti.SetCaret(idx)
}

func absoluteX(n tree.Node) float64 {
	x := 0.0
	for n != nil {
		x += n.Base().Bounds().X
		n = n.Base().Parent()
	}
	return x
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
