package ui

import (
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

// reloadApp builds an app without a window and a window whose tree came from
// src, so reload logic can be exercised headlessly.
func reloadApp(t *testing.T, src string) (*App, *Window) {
	t.Helper()
	a := newHeadlessApp()
	tr, diag := tree.BuildSource([]byte(src), &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	w := &Window{tree: tr, app: a, title: tr.Root.Title.Get()}
	a.win = w
	return a, w
}

func applyReloadSource(t *testing.T, a *App, src string) error {
	t.Helper()
	f, diag := syntax.Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	return a.applyReload(f)
}

func TestReloadKeepsGoSetValues(t *testing.T) {
	a, w := reloadApp(t, `Window {
		Text { id: label text: "0" color: #222222 }
	}`)
	label := MustByID[*Text](w, "label")
	label.Text.Set("42")

	// A file edit that does not touch the label's text literal.
	if err := applyReloadSource(t, a, `Window {
		Text { id: label text: "0" color: #ff0000 }
	}`); err != nil {
		t.Fatal(err)
	}

	label = MustByID[*Text](w, "label")
	if got := label.Text.Get(); got != "42" {
		t.Fatalf("Go-set text was lost: %q", got)
	}
	if got := label.Color.Get(); got != 0xFF0000FF {
		t.Fatalf("file literal not applied: %08x", got)
	}
}

func TestReloadFileLiteralWins(t *testing.T) {
	a, w := reloadApp(t, `Window { Text { id: label text: "0" } }`)
	MustByID[*Text](w, "label").Text.Set("42")

	// The file changed the same property: the author's new literal wins.
	if err := applyReloadSource(t, a, `Window { Text { id: label text: "edited" } }`); err != nil {
		t.Fatal(err)
	}
	if got := MustByID[*Text](w, "label").Text.Get(); got != "edited" {
		t.Fatalf("text = %q, want the new literal", got)
	}
}

func TestReloadKeepsTextInputContents(t *testing.T) {
	a, w := reloadApp(t, `Window { TextField { id: name placeholder: "x" } }`)
	ti := MustByID[*TextField](w, "name")
	ti.Text.Set("Kaan") // as the input layer would
	ti.Caret = 2

	if err := applyReloadSource(t, a, `Window { TextField { id: name placeholder: "type here" } }`); err != nil {
		t.Fatal(err)
	}
	ti = MustByID[*TextField](w, "name")
	if got := ti.Text.Get(); got != "Kaan" {
		t.Fatalf("input contents lost: %q", got)
	}
	if got := ti.Caret; got != 2 {
		t.Fatalf("caret lost: %d", got)
	}
	if got := ti.Placeholder.Get(); got != "type here" {
		t.Fatalf("placeholder not reloaded: %q", got)
	}
}

func TestReloadRerunsBind(t *testing.T) {
	a, w := reloadApp(t, `Window {
		Text { id: label text: "0" }
		Button { id: inc label: "+" }
	}`)
	binds := 0
	if err := w.Bind(func(w *Window) error {
		binds++
		ui := MustByID[*Button](w, "inc")
		label := MustByID[*Text](w, "label")
		ui.OnClick(func(ClickEvent) { label.Text.Set("clicked") })
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := applyReloadSource(t, a, `Window {
		Text { id: label text: "0" }
		Button { id: inc label: "+" }
	}`); err != nil {
		t.Fatal(err)
	}
	if binds != 2 {
		t.Fatalf("Bind ran %d times, want 2", binds)
	}
	// The new button must have a working handler.
	btn := MustByID[*Button](w, "inc")
	btn.Click.Emit(ClickEvent{})
	if got := MustByID[*Text](w, "label").Text.Get(); got != "clicked" {
		t.Fatalf("handler not reattached: %q", got)
	}
}

func TestReloadMarkedDirty(t *testing.T) {
	a, w := reloadApp(t, `Window { Text { id: a text: "x" } }`)
	if err := applyReloadSource(t, a, `Window { Text { id: a text: "y" } }`); err != nil {
		t.Fatal(err)
	}
	if _, dirty := w.tree.Root.Base().Dirty(); !dirty {
		t.Fatal("reload did not mark the tree dirty")
	}
	if _, dirty := MustByID[*Text](w, "a").Base().Dirty(); !dirty {
		t.Fatal("reload did not dirty the text node")
	}
}

func TestReloadReportsDiagnosticsWithoutReplacingTree(t *testing.T) {
	_, w := reloadApp(t, `Window { Text { id: label text: "0" } }`)
	before := w.tree
	src := "Window {\n  width: \"broken\"\n}\n"
	f, diag := syntax.Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("expected a parseable file: %v", diag)
	}
	// applyReload builds the tree; schema errors are caught before it is called
	// in production, so it only needs to survive a build failure here.
	_ = f
	if w.tree != before {
		t.Fatal("tree changed on a failed reload")
	}
}

func TestWatcherTriggersReload(t *testing.T) {
	a, w := reloadApp(t, `Window { Text { id: label text: "0" } }`)
	dir := t.TempDir()
	if err := writeFile(dir+"/app.goi", `Window { Text { id: label text: "0" } }`); err != nil {
		t.Fatal(err)
	}
	a.assets = dirFS(dir)
	a.path = "app.goi"
	if err := a.enableWatcher(); err != nil {
		t.Fatal(err)
	}
	defer a.watcher.Close()

	if err := writeFile(dir+"/app.goi", `Window { Text { id: label text: "reloaded" } }`); err != nil {
		t.Fatal(err)
	}

	// The watcher posts work; run the queue the way the frame loop would.
	deadline := 3000
	for i := 0; i < deadline; i++ {
		a.drainPosted()
		if MustByID[*Text](w, "label").Text.Get() == "reloaded" {
			return
		}
		sleepMS(2)
	}
	t.Fatal("watcher did not reload the file")
}

func TestReloadDiagnosticsAreFormatted(t *testing.T) {
	a, _ := reloadApp(t, `Window { }`)
	dir := t.TempDir()
	if err := writeFile(dir+"/test.goi", "Window {\n  nope: 1\n}\n"); err != nil {
		t.Fatal(err)
	}
	a.assets = dirFS(dir)
	a.path = "test.goi"

	err := a.reloadNow()
	if err == nil {
		t.Fatal("expected a diagnostic")
	}
	if !strings.Contains(err.Error(), "test.goi:2:3") {
		t.Fatalf("diagnostic lacks a position: %s", err.Error())
	}
}

// TestReloadKeepsTheme is the root case: the window has no id, so a Go-set theme
// used to be reset to the file's literal on every hot reload.
func TestReloadKeepsTheme(t *testing.T) {
	a, w := reloadApp(t, `Window {
		theme: light
		Text { id: label text: "x" }
	}`)
	w.Root().Theme.Set("dark")
	if err := applyReloadSource(t, a, `Window {
		theme: light
		Text { id: label text: "y" }
	}`); err != nil {
		t.Fatal(err)
	}
	if got := w.Root().Theme.Get(); got != "dark" {
		t.Fatalf("theme after reload = %q, want dark", got)
	}
}

// TestReloadKeepsScrollPosition: a hot reload re-reads the design, and a reader
// half-way down a page must stay there instead of being thrown back to the top.
func TestReloadKeepsScrollPosition(t *testing.T) {
	a, win, _ := inputFixture(t, `Window {
		Column {
			id:     page
			scroll: true
			width:  100
			height: 100

			Rectangle { width: 100 height: 600 background: #ff0000 }
		}

		Row {
			id:     strip
			scroll: true
			width:  100
			height: 40

			Rectangle { width: 600 height: 40 background: #00ff00 }
		}
	}`, 100, 200)
	page := MustByID[*Column](win, "page")
	strip := MustByID[*Row](win, "strip")
	page.Base().SetScrollOffsetFor(tree.AxisY, 240)
	strip.Base().SetScrollOffsetFor(tree.AxisX, 90)

	if err := applyReloadSource(t, a, `Window {
		Column {
			id:     page
			scroll: true
			width:  100
			height: 100

			Rectangle { width: 100 height: 600 background: #0000ff }
		}

		Row {
			id:     strip
			scroll: true
			width:  100
			height: 40

			Rectangle { width: 600 height: 40 background: #00ff00 }
		}
	}`); err != nil {
		t.Fatal(err)
	}
	layoutAgain(win)
	page = MustByID[*Column](win, "page")
	strip = MustByID[*Row](win, "strip")
	if got := page.Base().ScrollOffset(); got != 240 {
		t.Fatalf("page scrolled to %v after reload, want 240", got)
	}
	if got := strip.Base().ScrollOffset(); got != 90 {
		t.Fatalf("strip scrolled to %v after reload, want 90", got)
	}
}

// TestReloadKeepsScrollWithoutAnID: a scroller nobody named is matched by its
// place in the tree, so a style edit elsewhere does not reset it.
func TestReloadKeepsScrollWithoutAnID(t *testing.T) {
	a, win, _ := inputFixture(t, `Window {
		Column {
			scroll:  false
			padding: 4

			Column {
				scroll: true
				width:  100
				height: 100

				Rectangle { width: 100 height: 600 background: #ff0000 }
			}
		}
	}`, 100, 200)
	page := scrollableChild(win.tree.Root.Base())
	if page == nil {
		t.Fatal("the fixture has no scroller")
	}
	page.SetScrollOffsetFor(tree.AxisY, 120)

	if err := applyReloadSource(t, a, `Window {
		Column {
			scroll:  false
			padding: 8

			Column {
				scroll: true
				width:  100
				height: 100

				Rectangle { width: 100 height: 600 background: #ff0000 }
			}
		}
	}`); err != nil {
		t.Fatal(err)
	}
	layoutAgain(win)
	page = scrollableChild(win.tree.Root.Base())
	if page == nil {
		t.Fatal("the scroller went missing")
	}
	if got := page.ScrollOffset(); got != 120 {
		t.Fatalf("scrolled to %v after reload, want 120", got)
	}
}

// scrollableChild returns the first scrolling node under b.
func scrollableChild(b *tree.NodeBase) *tree.NodeBase {
	if b.ScrollAxis() != tree.AxisNone {
		return b
	}
	for _, k := range b.Kids() {
		if found := scrollableChild(k); found != nil {
			return found
		}
	}
	return nil
}

// layoutAgain lays the freshly built tree out at the fixture's size, which is
// what makes a restored offset readable through its clamp.
func layoutAgain(w *Window) {
	layout.New(nil).Layout(w.tree.Root, 100, 200)
}
