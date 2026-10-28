package ui

import (
	"sync"
	"testing"
	"time"

	"github.com/ksckaan1/goui/internal/layout"
	"github.com/ksckaan1/goui/internal/paint"
	"github.com/ksckaan1/goui/internal/tree"
)

// inputFixture builds a laid-out headless window plus an interactor wired to it.
func inputFixture(t *testing.T, src string, w, h float64) (*App, *Window, *interactor) {
	t.Helper()
	a, win := reloadApp(t, src)
	in := &interactor{app: a, tree: win.tree}
	// Headless: no driver, so give the interactor a fixed viewport.
	in.viewportW, in.viewportH = w, h
	layout.New(textMeasurer{}).Layout(win.tree.Root, w, h)
	return a, win, in
}

const inputUI = `Window {
	Column {
		padding: 10
		Text { id: label text: "0" }
		Button { id: btn width: 100 height: 40 label: "Go" }
		TextField { id: field width: 200 height: 30 }
	}
}`

func TestHitTestFindsDeepestNode(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	btn := MustByID[*Button](win, "btn")
	b := absoluteRect(btn)
	hit := in.hit(b.X+5, b.Y+5)
	if hit == nil {
		t.Fatal("nothing hit")
	}
	if hit.Kind() != "Button" {
		t.Fatalf("hit %s, want the button", hit.Kind())
	}
}

func TestClickPressRelease(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	btn := MustByID[*Button](win, "btn")
	b := absoluteRect(btn)
	cx, cy := b.X+b.W/2, b.Y+b.H/2

	clicks := 0
	btn.OnClick(func(ClickEvent) { clicks++ })

	in.MouseDown(cx, cy, 0, false)
	in.MouseUp(cx, cy, 0)
	if clicks != 1 {
		t.Fatalf("clicks = %d", clicks)
	}
}

func TestClickNeedsSameNode(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	btn := MustByID[*Button](win, "btn")
	b := absoluteRect(btn)
	clicks := 0
	btn.OnClick(func(ClickEvent) { clicks++ })

	in.MouseDown(b.X+5, b.Y+5, 0, false)
	in.MouseUp(b.X+5, b.Y+b.H+60, 0) // released elsewhere
	if clicks != 0 {
		t.Fatalf("click fired across nodes: %d", clicks)
	}
}

func TestHoverToggles(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	btn := MustByID[*Button](win, "btn")
	b := absoluteRect(btn)

	in.MouseMove(b.X+5, b.Y+5)
	if !btn.Hover {
		t.Fatal("button did not become hovered")
	}
	in.MouseMove(1, 300)
	if btn.Hover {
		t.Fatal("button stayed hovered")
	}
}

func TestTextInputTyping(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	b := absoluteRect(ti)

	var changes []string
	ti.OnEdit(func(s string) { changes = append(changes, s) })

	in.MouseDown(b.X+10, b.Y+10, 0, false) // focus it
	in.MouseUp(b.X+10, b.Y+10, 0)
	for _, r := range "Kaan" {
		in.Char(r)
	}
	if got := ti.Text.Get(); got != "Kaan" {
		t.Fatalf("text = %q", got)
	}
	if got := ti.Caret; got != 4 {
		t.Fatalf("caret = %d", got)
	}
	in.key(backspace, noMods)
	if got := ti.Text.Get(); got != "Kaa" {
		t.Fatalf("after backspace = %q", got)
	}
	if len(changes) != 5 {
		t.Fatalf("change events = %d: %v", len(changes), changes)
	}
}

func TestSearchBarTyping(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column { SearchBar { id: search width: 300 } }
	}`, 480, 320)
	bar := MustByID[*SearchBar](win, "search")
	b := absoluteRect(bar)
	in.MouseDown(b.X+10, b.Y+10, 0, false)
	in.MouseUp(b.X+10, b.Y+10, 0)
	for _, r := range "find" {
		in.Char(r)
	}
	if got := bar.Text.Get(); got != "find" {
		t.Fatalf("search text = %q after clicking and typing", got)
	}
	if in.focus != bar.M3() {
		t.Fatal("search bar did not take focus")
	}
}

func TestSegmentedButtonModes(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			Row {
				SegmentedButton { id: day label: "Day" width: 80 }
				SegmentedButton { id: week label: "Week" width: 80 selected: true }
			}
			Row {
				SegmentedButton { id: one label: "One" width: 80 variant: single selected: true }
				SegmentedButton { id: two label: "Two" width: 80 variant: single }
			}
		}
	}`, 480, 320)
	day, week := MustByID[*SegmentedButton](win, "day"), MustByID[*SegmentedButton](win, "week")
	one, two := MustByID[*SegmentedButton](win, "one"), MustByID[*SegmentedButton](win, "two")
	click := func(c *SegmentedButton) {
		b := absoluteRect(c)
		in.MouseDown(b.X+b.W/2, b.Y+b.H/2, 0, false)
		in.MouseUp(b.X+b.W/2, b.Y+b.H/2, 0)
	}

	// Multi is the default: a second click clears the segment, and siblings
	// keep their own state.
	click(week)
	if week.Selected.Get() {
		t.Fatal("multi segment did not toggle off")
	}
	click(day)
	if !day.Selected.Get() || week.Selected.Get() {
		t.Fatalf("multi selection = day:%v week:%v, want day only", day.Selected.Get(), week.Selected.Get())
	}

	// Single behaves like a radio group: picking another moves the selection,
	// and clicking the selected segment leaves it selected.
	click(two)
	if one.Selected.Get() || !two.Selected.Get() {
		t.Fatalf("single selection = one:%v two:%v, want two only", one.Selected.Get(), two.Selected.Get())
	}
	click(two)
	if !two.Selected.Get() {
		t.Fatal("single segment cleared itself; radio selection must stay")
	}
}

func TestTextInputCursorKeys(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	b := absoluteRect(ti)
	in.MouseDown(b.X+10, b.Y+10, 0, false)

	in.Char('ç') // two bytes: caret arithmetic must stay on rune boundaries
	in.Char('a')
	if got := ti.Caret; got != 3 {
		t.Fatalf("caret after typing = %d, want 3 bytes", got)
	}
	in.key(left, noMods)
	if got := ti.Caret; got != 2 {
		t.Fatalf("caret after left = %d, want 2 (before 'a')", got)
	}
	in.key(home, noMods)
	if got := ti.Caret; got != 0 {
		t.Fatalf("home caret = %d", got)
	}
	in.key(end, noMods)
	if got := ti.Caret; got != 3 {
		t.Fatalf("end caret = %d", got)
	}
}

func TestSubmitOnEnter(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	b := absoluteRect(ti)
	in.MouseDown(b.X+10, b.Y+10, 0, false)
	in.Char('x')
	var submitted string
	ti.OnSubmit(func(s string) { submitted = s })
	in.key(enter, noMods)
	if submitted != "x" {
		t.Fatalf("submitted = %q", submitted)
	}
}

func TestClickOutsideClearsFocus(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	b := absoluteRect(ti)
	in.MouseDown(b.X+10, b.Y+10, 0, false)
	if in.focus != ti.M3() {
		t.Fatal("input did not take focus")
	}
	in.MouseDown(470, 310, 0, false)
	if in.focus != nil {
		t.Fatal("focus did not clear")
	}
}

func TestPostRunsOnQueue(t *testing.T) {
	a := newHeadlessApp()
	var wg sync.WaitGroup
	wg.Add(1)
	ran := false
	go func() {
		defer wg.Done()
		a.Post(func() { ran = true })
	}()
	wg.Wait()
	if ran {
		t.Fatal("Post ran on the wrong goroutine")
	}
	a.drainPosted()
	if !ran {
		t.Fatal("posted function did not run")
	}
}

func TestPostIsFIFO(t *testing.T) {
	a := newHeadlessApp()
	var order []int
	for i := 0; i < 5; i++ {
		i := i
		a.Post(func() { order = append(order, i) })
	}
	a.drainPosted()
	for i, v := range order {
		if v != i {
			t.Fatalf("order = %v", order)
		}
	}
}

func TestPostConcurrentWithDrain(t *testing.T) {
	a := newHeadlessApp()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Post(func() {})
		}()
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			a.drainPosted()
			time.Sleep(time.Millisecond)
		}
		close(done)
	}()
	wg.Wait()
	<-done
	a.drainPosted()
	if len(a.posted) != 0 {
		t.Fatalf("queue not empty: %d", len(a.posted))
	}
}

func TestSetDirtyRequestsFrame(t *testing.T) {
	a, win := reloadApp(t, inputUI)
	a.needsFrame = false
	MustByID[*Text](win, "label").Text.Set("9")
	if !a.needsFrame {
		t.Fatal("a property change did not request a frame")
	}
}

func TestNoFrameWithoutChange(t *testing.T) {
	a, win := reloadApp(t, inputUI)
	a.needsFrame = false
	label := MustByID[*Text](win, "label")
	label.Text.Set(label.Text.Get()) // same value: no-op
	if a.needsFrame {
		t.Fatal("setting an identical value requested a frame")
	}
}

func TestInvisibleNodeIsNotHit(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Stack {
			Rectangle { id: under width: 100 height: 100 background: #ff0000 }
			Rectangle { id: over width: 100 height: 100 background: #00ff00 visible: false }
		}
	}`, 200, 200)
	hit := in.hit(50, 50)
	if hit == nil || hit.Base().ID() != "under" {
		t.Fatalf("hit = %v, want the visible rect", hit)
	}
	_ = win
}

func TestLastStackChildWinsHit(t *testing.T) {
	_, _, in := inputFixture(t, `Window {
		Stack {
			Rectangle { id: back width: 100 height: 100 background: #ff0000 }
			Rectangle { id: front width: 100 height: 100 background: #00ff00 }
		}
	}`, 200, 200)
	hit := in.hit(50, 50)
	if hit == nil || hit.Base().ID() != "front" {
		t.Fatalf("hit = %v, want the topmost rect", hit)
	}
}

// Keys and mods used by the headless tests.
var (
	backspace = tree.KeyBackspace
	left      = tree.KeyLeft
	home      = tree.KeyHome
	end       = tree.KeyEnd
	enter     = tree.KeyEnter
	noMods    tree.Mods
)

const scrollUI = `Window {
	Column {
		id: page
		height: 100
		scroll: true
		Column {
			height: 300
			padding: 30
			Button { id: inner width: 80 height: 40 label: "Row" }
		}
	}
}`

func TestWheelScrollsAndClamps(t *testing.T) {
	_, win, in := inputFixture(t, scrollUI, 400, 400)
	page, _ := win.tree.ByID("page")
	in.MouseMove(50, 50)

	in.Scroll(0, -1)
	if got := page.Base().ScrollOffset(); got != 48 {
		t.Fatalf("offset after one notch = %v, want 48", got)
	}
	in.Scroll(0, -100) // far past the end
	if got := page.Base().ScrollOffset(); got != 200 {
		t.Fatalf("offset = %v, want clamped to 200", got)
	}
	in.Scroll(0, 100)
	if got := page.Base().ScrollOffset(); got != 0 {
		t.Fatalf("offset = %v, want 0 after scrolling back", got)
	}
}

func TestDragScrollMovesContentAndCancelsClick(t *testing.T) {
	_, win, in := inputFixture(t, scrollUI, 400, 400)
	page, _ := win.tree.ByID("page")
	btn := MustByID[*Button](win, "inner")
	clicks := 0
	btn.OnClick(func(ClickEvent) { clicks++ })
	b := absoluteRect(btn)

	in.MouseDown(b.X+5, b.Y+5, 0, false)
	in.MouseMove(b.X+5, b.Y+5-40) // drag up: content follows the pointer
	if got := page.Base().ScrollOffset(); got != 40 {
		t.Fatalf("offset after drag = %v, want 40", got)
	}
	in.MouseUp(b.X+5, b.Y+5-40, 0)
	if clicks != 0 {
		t.Fatalf("drag fired a click: %d", clicks)
	}
	if btn.Pressed {
		t.Fatal("button stayed pressed after the drag")
	}
}

func TestClickInsideScrollAreaStillWorks(t *testing.T) {
	_, win, in := inputFixture(t, scrollUI, 400, 400)
	btn := MustByID[*Button](win, "inner")
	clicks := 0
	btn.OnClick(func(ClickEvent) { clicks++ })
	b := absoluteRect(btn)
	in.MouseDown(b.X+5, b.Y+5, 0, false)
	in.MouseUp(b.X+5, b.Y+5, 0)
	if clicks != 1 {
		t.Fatalf("clicks = %d, want 1", clicks)
	}
}

// scrolledHit follows the offset: a child that moved up must be hit at its new
// place, and not at the old one.
func TestHitFollowsScroll(t *testing.T) {
	_, win, in := inputFixture(t, scrollUI, 400, 400)
	page, _ := win.tree.ByID("page")
	btn := MustByID[*Button](win, "inner")
	b := absoluteRect(btn)
	page.Base().SetScrollOffset(30)
	if hit := in.hit(b.X+5, b.Y+5-30); hit != btn.Base().Node() {
		t.Fatalf("hit at the scrolled position = %v", hit)
	}
}

const rippleUI = `Window {
	Button { id: btn width: 120 height: 48 label: "Tap" }
}`

func TestRippleRunsAndFinishes(t *testing.T) {
	_, win, in := inputFixture(t, rippleUI, 200, 200)
	btn := MustByID[*Button](win, "btn")
	b := absoluteRect(btn)

	in.MouseDown(b.X+20, b.Y+10, 0, false)
	c := btn.M3()
	if !c.RippleOn {
		t.Fatal("press did not start a ripple")
	}
	if !in.tick(0.1) {
		t.Fatal("tick reported no animation")
	}
	if c.Ripple <= 0 || c.Ripple >= 1 {
		t.Fatalf("progress = %v", c.Ripple)
	}
	if c.RippleX != b.X+20 || c.RippleY != b.Y+10 {
		t.Fatalf("origin = %v,%v", c.RippleX, c.RippleY)
	}

	// The display list carries the expanding disc, clipped to the button shape.
	l := &paint.List{}
	l.Build(win.tree, 200, 200)
	var found bool
	for _, op := range l.Ops {
		if op.Shape == paint.Circle && op.ClipRadius > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no ripple op:\n%s", l.Dump())
	}

	in.MouseUp(b.X+20, b.Y+10, 0)
	if in.tick(1) {
		t.Fatal("ripple outlived its duration")
	}
	if c.RippleOn {
		t.Fatal("ripple still marked running")
	}
}

// focusField clicks the field at its left inset and returns its rect.
func focusField(t *testing.T, in *interactor, id string, win *Window) Rect {
	t.Helper()
	f := MustByID[*TextField](win, id)
	b := absoluteRect(f)
	in.MouseDown(b.X+16, b.Y+10, 0, false)
	in.MouseUp(b.X+16, b.Y+10, 0)
	return b
}

func TestSelectAllAndTypeReplaces(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	ti.Text.Set("hello")
	focusField(t, in, "field", win)

	in.key(tree.KeyA, tree.Mods{Ctrl: true})
	if a, b, ok := ti.Selection(); !ok || a != 0 || b != 5 {
		t.Fatalf("selection = %d..%d ok=%v, want 0..5", a, b, ok)
	}
	in.Char('x')
	if got := ti.Text.Get(); got != "x" {
		t.Fatalf("text = %q, want x", got)
	}
	if _, _, ok := ti.Selection(); ok {
		t.Fatal("selection survived typing")
	}
}

func TestShiftArrowSelectsAndBackspaceDeletes(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	ti.Text.Set("abc")
	focusField(t, in, "field", win)
	in.key(tree.KeyEnd, noMods)

	in.key(tree.KeyLeft, tree.Mods{Shift: true})
	if a, b, ok := ti.Selection(); !ok || a != 2 || b != 3 {
		t.Fatalf("selection = %d..%d ok=%v, want 2..3", a, b, ok)
	}
	in.key(tree.KeyBackspace, noMods)
	if got := ti.Text.Get(); got != "ab" {
		t.Fatalf("text = %q, want ab", got)
	}
	if ti.Caret != 2 {
		t.Fatalf("caret = %d, want 2", ti.Caret)
	}
}

func TestCopyCutAndPaste(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	var clip string
	in.getClipboard = func() string { return clip }
	in.setClipboard = func(s string) { clip = s }

	ti.Text.Set("hello")
	focusField(t, in, "field", win)
	in.key(tree.KeyA, tree.Mods{Ctrl: true})
	in.key(tree.KeyC, tree.Mods{Ctrl: true})
	if clip != "hello" {
		t.Fatalf("clipboard = %q, want hello", clip)
	}

	clip = "pasted"
	in.key(tree.KeyV, tree.Mods{Ctrl: true})
	if got := ti.Text.Get(); got != "pasted" {
		t.Fatalf("text after paste = %q, want pasted", got)
	}

	in.key(tree.KeyA, tree.Mods{Ctrl: true})
	in.key(tree.KeyX, tree.Mods{Ctrl: true})
	if got := ti.Text.Get(); got != "" {
		t.Fatalf("text after cut = %q, want empty", got)
	}
	if clip != "pasted" {
		t.Fatalf("clipboard after cut = %q, want pasted", clip)
	}
}

func TestPasteFlattensNewlines(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	in.getClipboard = func() string { return "one\ntwo" }
	focusField(t, in, "field", win)
	in.key(tree.KeyV, tree.Mods{Ctrl: true})
	if got := ti.Text.Get(); got != "one two" {
		t.Fatalf("text = %q, want one two", got)
	}
}

func TestDragSelectsText(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	ti.Text.Set("hello world")
	b := focusField(t, in, "field", win)
	in.MouseDown(b.X+16, b.Y+10, 0, false)
	in.MouseMove(b.X+16+5*8.8, b.Y+10)
	in.MouseUp(b.X+16+5*8.8, b.Y+10, 0)
	if a, e, ok := ti.Selection(); !ok || a != 0 || e != 5 {
		t.Fatalf("selection = %d..%d ok=%v, want 0..5", a, e, ok)
	}
}

func TestDoubleClickSelectsWord(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	ti.Text.Set("hello world")
	b := absoluteRect(ti)
	x := b.X + 16 + 2*8.8             // inside "hello"
	in.MouseDown(x, b.Y+10, 0, false) // first click: caret
	in.MouseUp(x, b.Y+10, 0)
	in.MouseDown(x, b.Y+10, 0, false) // second click: word selection
	in.MouseUp(x, b.Y+10, 0)
	if a, e, ok := ti.Selection(); !ok || a != 0 || e != 5 {
		t.Fatalf("selection = %d..%d ok=%v, want 0..5", a, e, ok)
	}
}

func TestCtrlArrowMovesByWord(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	ti.Text.Set("hello world")
	focusField(t, in, "field", win)
	in.key(tree.KeyHome, noMods)

	in.key(tree.KeyRight, tree.Mods{Ctrl: true})
	if ti.Caret != 5 {
		t.Fatalf("caret after ctrl+right = %d, want 5", ti.Caret)
	}
	in.key(tree.KeyRight, tree.Mods{Ctrl: true})
	if ti.Caret != 11 {
		t.Fatalf("caret after second ctrl+right = %d, want 11", ti.Caret)
	}
	in.key(tree.KeyLeft, tree.Mods{Ctrl: true})
	if ti.Caret != 6 {
		t.Fatalf("caret after ctrl+left = %d, want 6", ti.Caret)
	}
}

func TestClickPositionedButton(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Stack {
			background: #eeeeee
			Positioned {
				right: 20
				bottom: 20
				Button { id: fab width: 60 height: 40 label: "Go" }
			}
		}
	}`, 300, 200)
	btn := MustByID[*Button](win, "fab")
	b := absoluteRect(btn)
	if b.X != 220 || b.Y != 140 || b.W != 60 || b.H != 40 {
		t.Fatalf("button rect = %+v, want {220 140 60 40}", b)
	}

	clicks := 0
	btn.OnClick(func(ClickEvent) { clicks++ })
	in.MouseDown(b.X+b.W/2, b.Y+b.H/2, 0, false)
	in.MouseUp(b.X+b.W/2, b.Y+b.H/2, 0)
	if clicks != 1 {
		t.Fatalf("clicks = %d", clicks)
	}
}
