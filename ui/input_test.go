package ui

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/platform"
	"github.com/goi-gui/goi/internal/tree"
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

func TestChromeHitTestDragsTitleBarAndResizesEdges(t *testing.T) {
	// The button sits in the top-left corner on purpose: it is inside the band
	// where the frame would otherwise resize, and must keep its click.
	const src = `Window {
		Column {
			TitleBar {
				id: bar
				height: 40
				Button { id: min width: 40 height: 30 label: "x" }
				Text { text: "Title" }
			}
			Rectangle { width: 20 height: 20 }
		}
	}`
	_, win, in := inputFixture(t, src, 480, 320)
	in.resizeEdges = true
	bar := MustByID[*TitleBar](win, "bar")
	btn := MustByID[*Button](win, "min")

	// A control wins over the frame, even in the corner.
	bb := absoluteRect(btn)
	if got := in.chromeHitTest(bb.X+2, bb.Y+2); got != platform.HitTestNormal {
		t.Fatalf("corner button hit = %v, want normal", got)
	}
	// The empty part of the bar asks the compositor to move the window.
	b := absoluteRect(bar)
	if got := in.chromeHitTest(b.X+b.W/2, b.Y+b.H/2); got != platform.HitTestDrag {
		t.Fatalf("title bar hit = %v, want drag", got)
	}
	// The outer pixels resize, and a corner wins over an edge.
	for _, tc := range []struct {
		x, y float64
		want platform.HitTest
	}{
		{240, 1, platform.HitTestResizeTop},
		{1, 160, platform.HitTestResizeLeft},
		{479, 160, platform.HitTestResizeRight},
		{240, 319, platform.HitTestResizeBottom},
		{1, 319, platform.HitTestResizeBottomLeft},
		{479, 319, platform.HitTestResizeBottomRight},
		{479, 1, platform.HitTestResizeTopRight},
	} {
		if got := in.chromeHitTest(tc.x, tc.y); got != tc.want {
			t.Fatalf("hit at %v,%v = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
	// A decorated window keeps the platform borders; the client area is ours.
	in.resizeEdges = false
	if got := in.chromeHitTest(1, 319); got != platform.HitTestNormal {
		t.Fatalf("decorated corner hit = %v, want normal", got)
	}
}

// TestExposeAsksForAFrame covers the interactive-resize path: Wayland holds the
// configure until the client commits a buffer, so an expose must schedule a
// frame even though nothing in the tree changed.
func TestExposeAsksForAFrame(t *testing.T) {
	a, _, in := inputFixture(t, inputUI, 480, 320)
	a.needsFrame = false
	in.Expose()
	if !a.needsFrame {
		t.Fatal("expose did not ask for a frame")
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

// TestSearchBarCaretAndSelection checks the search bar maps pointer x to text
// indices from its own 52dp text inset, and that dragging makes a selection.
func TestSearchBarCaretAndSelection(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		SearchBar { id: search text: "hello world" width: 300 }
	}`, 480, 320)
	bar := MustByID[*SearchBar](win, "search")
	b := absoluteRect(bar)
	charW := 16 * 0.55

	// A press starts the caret at the clicked character, not 36dp to the left.
	in.MouseDown(b.X+52+charW*5, b.Y+b.H/2, 0, false)
	if got := bar.Caret; got != 5 {
		t.Fatalf("caret after click = %d, want 5", got)
	}

	// Drag back to the start selects "hello".
	in.MouseMove(b.X+52, b.Y+b.H/2)
	in.MouseUp(b.X+52, b.Y+b.H/2, 0)
	a, z, ok := bar.Selection()
	if !ok || a != 0 || z != 5 {
		t.Fatalf("selection = %d..%d ok=%v, want 0..5", a, z, ok)
	}
}

// TestTextFieldClickAfterScroll checks the click-to-index mapping follows the
// scrolled text: a press near the right edge lands at the end of the value.
func TestTextFieldClickAfterScroll(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		TextField { id: field width: 200 height: 56 }
	}`, 480, 320)
	field := MustByID[*TextField](win, "field")
	long := "a very long value that cannot fit inside one field"
	field.Text.Set(long)
	b := absoluteRect(field)

	in.MouseDown(b.X+10, b.Y+10, 0, false) // focus, caret at 0
	for _, r := range long {
		in.Char(r)
	}
	// Simulate the paint pass that keeps the caret in view.
	field.M3().TextScroll = 0
	l := &paint.List{Measure: in.measure}
	l.Build(win.Tree(), 480, 320)

	// A press at the right edge of the value area maps to the end of the text.
	box := absoluteRect(field)
	in.MouseDown(box.X+box.W-20, box.Y+box.H/2, 0, false)
	if got := field.Caret; got < len(long)-2 {
		t.Fatalf("caret = %d after clicking the visible end, want near %d", got, len(long))
	}
	if field.M3().TextScroll <= 0 {
		t.Fatalf("scroll = %v, want the value shifted", field.M3().TextScroll)
	}
}

// TestTextFieldDragAutoScroll checks holding a selection drag past the right
// edge keeps scrolling, so the pointer can reach text that started off-screen.
func TestTextFieldDragAutoScroll(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		TextField { id: field width: 200 height: 56 }
	}`, 480, 320)
	field := MustByID[*TextField](win, "field")
	long := "one two three four five six seven eight nine ten eleven twelve"
	field.Text.Set(long)
	b := absoluteRect(field)

	in.MouseDown(b.X+10, b.Y+10, 0, false)
	for _, r := range long {
		in.Char(r)
	}
	rebuild := func() {
		l := &paint.List{Measure: in.measure}
		l.Build(win.Tree(), 480, 320)
	}
	rebuild()

	// Start a selection near the left, then hold past the right edge.
	in.MouseDown(b.X+20, b.Y+b.H/2, 0, false)
	in.MouseMove(b.X+b.W+40, b.Y+b.H/2)
	first := field.Caret
	scroll := field.M3().TextScroll
	for i := 0; i < 20; i++ {
		if !in.tick(1.0 / 60) {
			t.Fatal("tick stopped scrolling while the pointer was past the edge")
		}
		rebuild()
	}
	if field.M3().TextScroll <= scroll {
		t.Fatalf("scroll = %v, want more than the first frame's %v", field.M3().TextScroll, scroll)
	}
	if field.Caret <= first {
		t.Fatalf("caret = %d, want beyond the first frame's %d", field.Caret, first)
	}

	// Moving the pointer back inside stops the auto-scroll.
	in.MouseMove(b.X+b.W/2, b.Y+b.H/2)
	if in.tick(1.0 / 60) {
		t.Fatal("auto-scroll kept running after the pointer returned inside")
	}
}

// TestIMEPreeditAndCommit checks the composition lifecycle: preedit stays out of
// the value, a commit replaces it, and leaving the field drops it.
func TestIMEPreeditAndCommit(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			TextField { id: field width: 200 height: 56 }
			TextField { id: other width: 200 height: 56 }
		}
	}`, 480, 320)
	field := MustByID[*TextField](win, "field")
	b := absoluteRect(field)
	in.MouseDown(b.X+10, b.Y+10, 0, false) // focus it

	in.Char('a')
	in.Preedit("にほん", 9) // three runes, caret at the end
	if got := field.Preedit; got != "にほん" {
		t.Fatalf("preedit = %q", got)
	}
	if got := field.Text.Get(); got != "a" {
		t.Fatalf("value = %q, composition must not touch it", got)
	}

	// A commit lands where the composition sat and clears it.
	in.CommitText("日本")
	if got := field.Text.Get(); got != "a日本" {
		t.Fatalf("value after commit = %q", got)
	}
	if field.Preedit != "" {
		t.Fatalf("preedit = %q after commit, want empty", field.Preedit)
	}
	if got := field.Caret; got != len("a日本") {
		t.Fatalf("caret = %d, want %d", got, len("a日本"))
	}

	// Escape or a click elsewhere drops an unfinished composition.
	in.Preedit("か", 3)
	other := MustByID[*TextField](win, "other")
	ob := absoluteRect(other)
	in.MouseDown(ob.X+10, ob.Y+10, 0, false)
	if field.Preedit != "" {
		t.Fatalf("preedit = %q after leaving the field", field.Preedit)
	}
}

// TestIMEDeleteSurrounding checks the correction path an IME uses after it has
// already committed text.
func TestIMEDeleteSurrounding(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		TextField { id: field width: 200 height: 56 }
	}`, 480, 320)
	field := MustByID[*TextField](win, "field")
	b := absoluteRect(field)
	in.MouseDown(b.X+10, b.Y+10, 0, false)
	for _, r := range "hello world" {
		in.Char(r)
	}

	in.DeleteSurrounding(5, 0) // drop "world"
	if got := field.Text.Get(); got != "hello " {
		t.Fatalf("after delete before = %q", got)
	}
	if got := field.Caret; got != len("hello ") {
		t.Fatalf("caret = %d", got)
	}

	in.key(home, noMods)
	in.DeleteSurrounding(0, len("hello "))
	if got := field.Text.Get(); got != "" {
		t.Fatalf("after delete after = %q", got)
	}
}

// TestNavigationItemSelectionMovesTheIndicator checks that clicking a
// destination selects it, clears its siblings and reports the index to the
// container, which is what a page switcher binds to.
func TestNavigationItemSelectionMovesTheIndicator(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			NavigationRail {
				id: rail
				NavigationItem { id: one label: "One" icon: home selected: true }
				NavigationItem { id: two label: "Two" icon: star }
			}
		}
	}`, 480, 320)
	rail := MustByID[*NavigationRail](win, "rail")
	one, two := MustByID[*NavigationItem](win, "one"), MustByID[*NavigationItem](win, "two")

	var selected []int
	rail.OnSelect(func(i int) { selected = append(selected, i) })

	click := func(c *NavigationItem) {
		b := absoluteRect(c)
		in.MouseDown(b.X+b.W/2, b.Y+b.H/2, 0, false)
		in.MouseUp(b.X+b.W/2, b.Y+b.H/2, 0)
	}

	click(two)
	if !two.Selected.Get() || one.Selected.Get() {
		t.Fatalf("after click: one=%v two=%v, want only two", one.Selected.Get(), two.Selected.Get())
	}
	if len(selected) != 1 || selected[0] != 1 {
		t.Fatalf("select events = %v, want [1]", selected)
	}

	// Clicking the active destination is a no-op: a page must not reload itself.
	click(two)
	if len(selected) != 1 {
		t.Fatalf("select events = %v after clicking the active item", selected)
	}
}

// TestNavigationIndexCountsOnlyDestinations keeps a rail's own children (a
// header, a spacer, a theme switch) out of the index, because an app maps
// Select straight onto its page list.
func TestNavigationIndexCountsOnlyDestinations(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			NavigationRail {
				id: rail
				Text { text: "Header" }
				NavigationItem { id: one label: "One" icon: home selected: true }
				Rectangle { height: 8 }
				NavigationItem { id: two label: "Two" icon: star }
				Switch { id: theme }
			}
		}
	}`, 480, 320)
	rail := MustByID[*NavigationRail](win, "rail")
	two := MustByID[*NavigationItem](win, "two")

	var selected []int
	rail.OnSelect(func(i int) { selected = append(selected, i) })

	b := absoluteRect(two)
	in.MouseDown(b.X+b.W/2, b.Y+b.H/2, 0, false)
	in.MouseUp(b.X+b.W/2, b.Y+b.H/2, 0)
	if len(selected) != 1 || selected[0] != 1 {
		t.Fatalf("select events = %v, want [1] (the second destination)", selected)
	}
}

func TestTextInputWordEditing(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	b := absoluteRect(ti)
	ctrl := tree.Mods{Ctrl: true}
	typeWord := func(s string) {
		for _, r := range s {
			in.Char(r)
		}
	}
	reset := func() {
		ti.Text.Set("")
		ti.Caret = 0
	}

	in.MouseDown(b.X+10, b.Y+10, 0, false) // focus it

	// Ctrl+Backspace deletes the word before the caret, and with it the space
	// that separated it from the previous word.
	typeWord("hello big world")
	in.key(backspace, ctrl)
	if got := ti.Text.Get(); got != "hello big " {
		t.Fatalf("ctrl+backspace = %q", got)
	}
	in.key(backspace, ctrl)
	if got := ti.Text.Get(); got != "hello " {
		t.Fatalf("second ctrl+backspace = %q", got)
	}

	// Ctrl+Delete deletes forward, word by word.
	reset()
	typeWord("alpha beta gamma")
	in.key(home, noMods)
	in.key(tree.KeyDelete, ctrl)
	if got := ti.Text.Get(); got != "beta gamma" {
		t.Fatalf("ctrl+delete = %q", got)
	}
	in.key(tree.KeyDelete, ctrl)
	if got := ti.Text.Get(); got != "gamma" {
		t.Fatalf("second ctrl+delete = %q", got)
	}

	// Ctrl+Backspace at the start, and Ctrl+Delete at the end, are no-ops.
	reset()
	typeWord("one")
	in.key(home, noMods)
	in.key(backspace, ctrl)
	in.key(end, noMods)
	in.key(tree.KeyDelete, ctrl)
	if got := ti.Text.Get(); got != "one" {
		t.Fatalf("edge word deletes changed %q", got)
	}

	// Alt is the other word-delete modifier on Linux.
	alt := tree.Mods{Alt: true}
	reset()
	typeWord("hello big world")
	in.key(backspace, alt)
	if got := ti.Text.Get(); got != "hello big " {
		t.Fatalf("alt+backspace = %q", got)
	}
	in.key(home, noMods)
	in.key(tree.KeyDelete, alt)
	if got := ti.Text.Get(); got != "big " {
		t.Fatalf("alt+delete = %q", got)
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

func TestPlainNodeClick(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Stack {
			Rectangle { id: scrim }
		}
	}`, 480, 320)
	scrim := MustByID[*Rectangle](win, "scrim")
	clicks := 0
	scrim.OnClick(func(ClickEvent) { clicks++ })
	b := absoluteRect(scrim)
	in.MouseDown(b.X+10, b.Y+10, 0, false)
	in.MouseUp(b.X+10, b.Y+10, 0)
	if clicks != 1 {
		t.Fatalf("plain node clicks = %d, want 1", clicks)
	}
}

func TestWindowKeyHandlerRunsWithoutFocus(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Column { } }`, 480, 320)
	if in.focus != nil {
		t.Fatal("fixture already has focus")
	}
	var keys []tree.Key
	win.OnKey(func(e KeyEvent) { keys = append(keys, e.Key) })
	in.key(escape, noMods)
	if len(keys) != 1 || keys[0] != KeyEscape {
		t.Fatalf("window keys = %v, want one escape", keys)
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
	escape    = tree.KeyEscape
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

	in.Scroll(0, -1, false)
	if got := page.Base().ScrollOffset(); got != 48 {
		t.Fatalf("offset after one notch = %v, want 48", got)
	}
	in.Scroll(0, -100, false) // far past the end
	if got := page.Base().ScrollOffset(); got != 200 {
		t.Fatalf("offset = %v, want clamped to 200", got)
	}
	in.Scroll(0, 100, false)
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

// TestTextFieldTypeAndMask: the properties filter what reaches the value, both
// alone and together, and backspace skips the mask's literals.
func TestTextFieldTypeAndMask(t *testing.T) {
	const src = `Window {
		Column {
			padding: 10
			TextField { id: phone width: 220 height: 56 type: numeric mask: "(###) ###-####" }
			TextField { id: code width: 220 height: 56 type: "ABC123" }
			TextField { id: plain width: 220 height: 56 }
		}
	}`
	_, win, in := inputFixture(t, src, 480, 320)
	phone := MustByID[*TextField](win, "phone")
	focusField(t, in, "phone", win)
	for _, r := range "5551234567x" {
		in.Char(r)
	}
	if got := phone.Text.Get(); got != "(555) 123-4567" {
		t.Fatalf("phone = %q", got)
	}
	in.key(tree.KeyBackspace, tree.Mods{})
	if got := phone.Text.Get(); got != "(555) 123-456" {
		t.Fatalf("phone after backspace = %q", got)
	}

	code := MustByID[*TextField](win, "code")
	focusField(t, in, "code", win)
	for _, r := range "A1x2B3-" {
		in.Char(r)
	}
	if got := code.Text.Get(); got != "A12B3" {
		t.Fatalf("code = %q", got)
	}

	plain := MustByID[*TextField](win, "plain")
	focusField(t, in, "plain", win)
	for _, r := range "a1-_" {
		in.Char(r)
	}
	if got := plain.Text.Get(); got != "a1-_" {
		t.Fatalf("plain = %q", got)
	}
}

// TestTextFieldMaskEscape: a .goi string spells the mask's backslash twice, and
// the escaped slot character shows as punctuation.
func TestTextFieldMaskEscape(t *testing.T) {
	const src = `Window {
		Column {
			padding: 10
			TextField { id: issue width: 220 height: 56 type: numeric mask: "\\# ###" }
		}
	}`
	_, win, in := inputFixture(t, src, 480, 320)
	issue := MustByID[*TextField](win, "issue")
	focusField(t, in, "issue", win)
	for _, r := range "abc123" {
		in.Char(r)
	}
	if got := issue.Text.Get(); got != "# 123" {
		t.Fatalf("issue = %q, want # 123", got)
	}
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

// TestTabFocusAndEnter is the keyboard path: Tab walks the visible controls and
// Enter presses the focused one, so the app is usable without a pointer.
func TestTabFocusAndEnter(t *testing.T) {
	src := `Window {
		Column {
			Button { id: a label: "A" width: 100 height: 40 }
			Button { id: b label: "B" width: 100 height: 40 }
			TextField { id: f width: 100 height: 40 }
		}
	}`
	_, win, in := inputFixture(t, src, 400, 300)
	a, b := MustByID[*Button](win, "a"), MustByID[*Button](win, "b")
	pressed := ""
	a.OnClick(func(ClickEvent) { pressed = "a" })
	b.OnClick(func(ClickEvent) { pressed = "b" })

	in.key(tree.KeyTab, tree.Mods{})
	if in.kbd == nil || in.kbd != a.Node() {
		t.Fatalf("first Tab focused %v", in.kbd)
	}
	in.key(tree.KeyEnter, tree.Mods{})
	if pressed != "a" {
		t.Fatalf("Enter pressed %q", pressed)
	}
	in.key(tree.KeyTab, tree.Mods{})
	if in.kbd != b.Node() {
		t.Fatalf("second Tab focused %v", in.kbd)
	}
	in.key(tree.KeySpace, tree.Mods{})
	if pressed != "b" {
		t.Fatalf("Space pressed %q", pressed)
	}
	in.key(tree.KeyTab, tree.Mods{Shift: true})
	if in.kbd != a.Node() {
		t.Fatalf("shift-Tab focused %v", in.kbd)
	}
	// The text field is reachable and takes character focus when Tab lands there.
	in.key(tree.KeyTab, tree.Mods{})
	in.key(tree.KeyTab, tree.Mods{})
	if in.kbd != MustByID[*TextField](win, "f").Node() {
		t.Fatalf("third Tab focused %v", in.kbd)
	}
	if in.focus == nil {
		t.Fatal("Tab did not give the field character focus")
	}
}

// TestSliderArrowKeys covers keyboard access: a focused slider steps with the
// arrows, jumps ten steps with Shift, and snaps to a declared step.
func TestSliderArrowKeys(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			Slider { id: s value: 0.5 min: 0 max: 1 }
			Slider { id: p value: 50 min: 0 max: 100 step: 10 }
		}
	}`, 480, 320)
	s := MustByID[*Slider](win, "s")
	p := MustByID[*Slider](win, "p")
	var changes []float64
	s.OnChangeValue(func(v float64) { changes = append(changes, v) })

	in.setKbd(s.Base().Node())
	in.key(tree.KeyRight, tree.Mods{})
	if got := s.Value.Get(); math.Abs(got-0.55) > 1e-9 {
		t.Fatalf("right = %v, want 0.55", got)
	}
	in.key(tree.KeyLeft, tree.Mods{Shift: true})
	if got := s.Value.Get(); math.Abs(got-0.05) > 1e-9 {
		t.Fatalf("shift+left = %v, want 0.05", got)
	}
	in.key(tree.KeyEnd, tree.Mods{})
	if got := s.Value.Get(); got != 1 {
		t.Fatalf("end = %v, want 1", got)
	}
	in.key(tree.KeyRight, tree.Mods{})
	if got := s.Value.Get(); got != 1 {
		t.Fatalf("past the end = %v, want the clamp at 1", got)
	}
	if len(changes) != 3 {
		t.Fatalf("change events = %v, want three (a clamp is not a change)", changes)
	}

	in.setKbd(p.Base().Node())
	in.key(tree.KeyLeft, tree.Mods{})
	if got := p.Value.Get(); got != 40 {
		t.Fatalf("step slider = %v, want 40", got)
	}
}

// TestZIndexDrivesHitOrder keeps input consistent with paint: the node drawn on
// top is the one a press finds.
func TestZIndexDrivesHitOrder(t *testing.T) {
	_, _, in := inputFixture(t, `Window { Stack {
		width: 100
		height: 100
		Rectangle { id: under width: 100 height: 100 background: #ff0000 zIndex: -1 }
		Rectangle { id: over width: 100 height: 100 background: #00ff00 }
	} }`, 100, 100)
	hit := in.hit(50, 50)
	if hit == nil || hit.Base().ID() != "over" {
		t.Fatalf("hit = %v, want the topmost sibling", hit)
	}
}

// TestSliderRangeDragTakesNearestHandle covers the range variant from the
// pointer: a press moves the nearer handle, and the two never cross.
func TestSliderRangeDragTakesNearestHandle(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Column {
		Slider { id: r variant: range value: 2 end: 7 min: 0 max: 10 step: 1 width: 200 }
	} }`, 480, 320)
	s := MustByID[*Slider](win, "r")
	var ranges [][2]float64
	s.OnRange(func(lo, hi float64) { ranges = append(ranges, [2]float64{lo, hi}) })

	press := func(frac float64) {
		r := absoluteRect(s)
		x := r.X + 2 + (r.W-4)*frac
		in.MouseDown(x, r.Y+r.H/2, 0, false)
		in.MouseUp(x, r.Y+r.H/2, 0)
	}
	// 0.9 is nearer the high handle.
	press(0.9)
	if lo, hi := s.Range(); lo != 2 || hi != 9 {
		t.Fatalf("range = %v..%v, want 2..9", lo, hi)
	}
	// 0.3 is nearer the low one.
	press(0.3)
	if lo, hi := s.Range(); lo != 3 || hi != 9 {
		t.Fatalf("range = %v..%v, want 3..9", lo, hi)
	}
	// Dragging the low handle past the high one stops at it.
	r := absoluteRect(s)
	x := func(frac float64) float64 { return r.X + 2 + (r.W-4)*frac }
	in.MouseDown(x(0.3), r.Y+r.H/2, 0, false)
	in.MouseMove(x(0.99), r.Y+r.H/2)
	in.MouseUp(x(0.99), r.Y+r.H/2, 0)
	if lo, hi := s.Range(); lo != 9 || hi != 9 {
		t.Fatalf("range = %v..%v, want the low handle clamped at 9", lo, hi)
	}
	if len(ranges) == 0 {
		t.Fatal("a range drag emitted no range event")
	}
	last := ranges[len(ranges)-1]
	if last[0] > last[1] {
		t.Fatalf("range event = %v, want low first", last)
	}
}

// TestSliderRangeGoSide covers SetRange from Go: it clamps and keeps the pair
// in order.
func TestSliderRangeGoSide(t *testing.T) {
	_, win, _ := inputFixture(t, `Window { Column {
		Slider { id: r variant: range value: 2 end: 7 min: 0 max: 10 }
	} }`, 480, 320)
	s := MustByID[*Slider](win, "r")
	s.SetRange(8, 3)
	if lo, hi := s.Range(); lo != 3 || hi != 8 {
		t.Fatalf("range = %v..%v, want 3..8", lo, hi)
	}
	s.SetRange(-4, 20)
	if lo, hi := s.Range(); lo != 0 || hi != 10 {
		t.Fatalf("range = %v..%v, want it clamped to 0..10", lo, hi)
	}
}

// TestCodeCopyRipples checks that the copy button answers a press with the same
// ripple every other tappable surface shows, and still copies.
func TestCodeCopyRipples(t *testing.T) {
	const src = "Window { Column {\n" +
		"\tCode { id: c showCopy: true width: 400 height: 200 }\n" +
		"} }"
	_, win, in := inputFixture(t, src, 500, 400)
	code := MustByID[*Code](win, "c")
	code.Text.Set("package main\n\nfunc main() {}\n")

	btn := absoluteRect(code)
	r := paint.CodeCopyRect(btn)
	in.MouseDown(r.X+r.W/2, r.Y+r.H/2, 0, false)
	c := code.M3()
	if !c.RippleOn || c.Ripple <= 0 {
		t.Fatalf("copy press left ripple off: on=%v p=%v", c.RippleOn, c.Ripple)
	}
	if c.RippleX < r.X || c.RippleX > r.X+r.W || c.RippleY < r.Y || c.RippleY > r.Y+r.H {
		t.Fatalf("ripple started at %v,%v, outside the button %+v", c.RippleX, c.RippleY, r)
	}
	// The ripple finishes on its own and the flag clears.
	for i := 0; i < 50 && c.RippleOn; i++ {
		in.tick(0.02)
	}
	if c.RippleOn {
		t.Fatal("the ripple never finished")
	}
}

// TestTooltipHoversWithItsChild is the tooltip contract at the input layer: the
// wrapper is not a control, but hovering any part of the child it wraps marks it
// hovered, which is what paints the bubble.
func TestTooltipHoversWithItsChild(t *testing.T) {
	const src = `Window { Column {
		padding: 40
		Tooltip { id: tip text: "A hint"
			Text { id: inner text: "Hover me" }
		}
	} }`
	_, win, in := inputFixture(t, src, 480, 320)
	tip := MustByID[*Tooltip](win, "tip")
	inner := MustByID[*Text](win, "inner")
	b := absoluteRect(inner)

	in.MouseMove(b.X+2, b.Y+2)
	if !tip.M3().Hover {
		t.Fatal("hovering the child did not show the tooltip")
	}
	in.MouseMove(2, 300)
	if tip.M3().Hover {
		t.Fatal("the tooltip stayed hovered after the pointer left")
	}
}

// TestVideoHidesTheCursorWhenIdle is the picture rule: a pointer that rests on a
// video for the control bar's three seconds disappears, so it stops sitting on
// the film, and the next move brings it back.
func TestVideoHidesTheCursorWhenIdle(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Video { id: v width: 200 height: 120 } }`, 480, 320)
	v := MustByID[*Video](win, "v")
	var got platform.Cursor
	in.setCursor = func(c platform.Cursor) { got = c }

	b := absoluteRect(v)
	in.MouseMove(b.X+5, b.Y+5)
	if got == platform.CursorNone {
		t.Fatal("the cursor hid on a move")
	}
	in.hideMediaCursor(v)
	if got != platform.CursorNone {
		t.Fatalf("cursor = %v after idle, want it hidden", got)
	}
	in.MouseMove(b.X+6, b.Y+5)
	if got == platform.CursorNone {
		t.Fatal("the cursor stayed hidden after a move")
	}

	// Another element's idle clock must not hide a pointer that is elsewhere.
	outside := MustByID[*Video](win, "v")
	in.MouseMove(2, 300)
	in.hideMediaCursor(outside.Node())
	if got == platform.CursorNone {
		t.Fatal("a video away from the pointer hid it")
	}
}

const gestureUI = `Window {
	Column {
		padding: 20
		GestureDetector {
			id: gd
			width: 240
			height: 120

			Text { text: "gesture" }
		}
	}
}`

// TestGestureDetectorDoubleTap: a second press and release inside the double-tap
// window on the same detector fires DoubleTap, and the click that the second
// release would otherwise run is suppressed.
func TestGestureDetectorDoubleTap(t *testing.T) {
	_, win, in := inputFixture(t, gestureUI, 480, 320)
	gd := MustByID[*GestureDetector](win, "gd")
	b := absoluteRect(gd)
	doubles, clicks := 0, 0
	gd.OnDoubleTap(func(ClickEvent) { doubles++ })
	gd.OnClick(func(ClickEvent) { clicks++ })

	cx, cy := b.X+b.W/2, b.Y+b.H/2
	for i := 0; i < 2; i++ {
		in.MouseDown(cx, cy, 0, false)
		in.MouseUp(cx, cy, 0)
	}
	if doubles != 1 {
		t.Fatalf("double taps = %d, want 1", doubles)
	}
	if clicks != 1 {
		t.Fatalf("clicks = %d, want only the first tap", clicks)
	}
}

// TestGestureDetectorLongPress: a press that stays inside the slop becomes a
// long press, and its release does not also click.
func TestGestureDetectorLongPress(t *testing.T) {
	_, win, in := inputFixture(t, gestureUI, 480, 320)
	gd := MustByID[*GestureDetector](win, "gd")
	b := absoluteRect(gd)
	longs, clicks := 0, 0
	gd.OnLongPress(func(ClickEvent) { longs++ })
	gd.OnClick(func(ClickEvent) { clicks++ })

	cx, cy := b.X+b.W/2, b.Y+b.H/2
	in.MouseDown(cx, cy, 0, false)
	in.stopGestureTimer() // the test drives the clock instead of waiting
	in.fireLongPress(gd)
	in.MouseUp(cx, cy, 0)
	if longs != 1 {
		t.Fatalf("long presses = %d, want 1", longs)
	}
	if clicks != 0 {
		t.Fatalf("clicks = %d, want none after a long press", clicks)
	}
}

// TestGestureDetectorPan: leaving the slop starts a pan, each move reports its
// delta, and the release ends it. The click never fires.
func TestGestureDetectorPan(t *testing.T) {
	_, win, in := inputFixture(t, gestureUI, 480, 320)
	gd := MustByID[*GestureDetector](win, "gd")
	b := absoluteRect(gd)
	var starts, updates, ends int
	var total, last uiPanEvent
	gd.OnPanStart(func(e PanEvent) {
		starts++
		if e.DX != 0 || e.DY != 0 {
			t.Errorf("pan start carried a delta: %+v", e)
		}
	})
	gd.OnPanUpdate(func(e PanEvent) {
		updates++
		total.dx += e.DX
		total.dy += e.DY
		last = uiPanEvent{e.DX, e.DY}
	})
	gd.OnPanEnd(func(PanEvent) { ends++ })

	x0, y0 := b.X+10, b.Y+10
	in.MouseDown(x0, y0, 0, false)
	in.MouseMove(x0+30, y0)
	if starts != 1 || updates != 1 || last.dx != 30 {
		t.Fatalf("pan start = %d, updates %d, step %+v", starts, updates, last)
	}
	in.MouseMove(x0+45, y0)
	if updates != 2 || last.dx != 15 || total.dx != 45 {
		t.Fatalf("pan updates = %d, step %+v total %+v", updates, last, total)
	}
	in.MouseUp(x0+45, y0, 0)
	if ends != 1 {
		t.Fatalf("pan ends = %d", ends)
	}
}

// uiPanEvent is the two numbers a pan step carries, so a test can compare them.
type uiPanEvent struct{ dx, dy float64 }

// TestSeekbarDragScrubsOnRelease: a press on the scrubber starts a scrub, the
// thumb follows the pointer without reopening the decoder, and the release is
// the one seek.
func TestSeekbarDragScrubsOnRelease(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Video { id: v width: 320 height: 200 } }`, 480, 320)
	v := MustByID[*Video](win, "v")
	v.Duration = 100 * time.Second
	v.ControlsShown = true
	seeks := 0
	var seeked time.Duration
	v.Seeked.On(func(at time.Duration) { seeks++; seeked = at })

	r := absoluteRect(v)
	lay := paint.MediaLayoutOf(&v.Media, r)
	if lay.Track.W == 0 {
		t.Fatal("the bar laid out empty")
	}
	midY := lay.Track.Y + lay.Track.H/2
	x25 := lay.Track.X + lay.Track.W*0.25
	x75 := lay.Track.X + lay.Track.W*0.75

	in.MouseDown(x25, midY, 0, false)
	if !v.Media.Scrubbing || v.Position < 24*time.Second || v.Position > 26*time.Second {
		t.Fatalf("press: scrubbing=%v position=%v", v.Media.Scrubbing, v.Position)
	}
	in.MouseMove(x75, midY)
	if v.Position < 74*time.Second || v.Position > 76*time.Second {
		t.Fatalf("drag position = %v, want 75s", v.Position)
	}
	if seeks != 0 {
		t.Fatalf("the drag sought %d times before the release", seeks)
	}
	in.MouseUp(x75, midY, 0)
	if v.Media.Scrubbing {
		t.Fatal("the scrub did not end")
	}
	if seeks != 1 || seeked < 74*time.Second || seeked > 76*time.Second {
		t.Fatalf("release seeks=%d at=%v, want one seek to 75s", seeks, seeked)
	}
}
