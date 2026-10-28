package ui

import (
	"sync"
	"testing"
	"time"

	"github.com/ksckaan1/goui/internal/layout"
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

	in.MouseDown(cx, cy, 0)
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

	in.MouseDown(b.X+5, b.Y+5, 0)
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

	in.MouseDown(b.X+10, b.Y+10, 0) // focus it
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

func TestTextInputCursorKeys(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	ti := MustByID[*TextField](win, "field")
	b := absoluteRect(ti)
	in.MouseDown(b.X+10, b.Y+10, 0)

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
	in.MouseDown(b.X+10, b.Y+10, 0)
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
	in.MouseDown(b.X+10, b.Y+10, 0)
	if in.focus != ti.M3() {
		t.Fatal("input did not take focus")
	}
	in.MouseDown(470, 310, 0)
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
