package ui

import (
	"strconv"
	"testing"

	"github.com/ksckaan1/goui/internal/layout"
	"github.com/ksckaan1/goui/internal/syntax"
)

// counterApp is the example's state, declared outside Bind so it survives a
// reload. This test walks the whole loop: click, count, reload, click again.
type counterApp struct{ n int }

func TestCounterSurvivesReload(t *testing.T) {
	src := `Window {
		Column {
			Text { id: label text: "0" }
			Button { id: inc width: 100 height: 40 label: "+" }
		}
	}`
	a, win := reloadApp(t, src)
	in := &interactor{app: a, tree: win.tree, viewportW: 480, viewportH: 320}
	layout.New(textMeasurer{}).Layout(win.tree.Root, 480, 320)

	c := &counterApp{}
	bind := func(w *Window) error {
		label := MustByID[*Text](w, "label")
		inc := MustByID[*Button](w, "inc")
		inc.OnClick(func(ClickEvent) {
			c.n++
			label.Text.Set(strconv.Itoa(c.n))
		})
		return nil
	}
	if err := win.Bind(bind); err != nil {
		t.Fatal(err)
	}

	click := func() {
		b := absoluteRect(MustByID[*Button](win, "inc"))
		in.MouseDown(b.X+5, b.Y+5, 0)
		in.MouseUp(b.X+5, b.Y+5, 0)
	}

	click()
	click()
	if c.n != 2 {
		t.Fatalf("count = %d", c.n)
	}

	// Reload with a cosmetic change; the handler must be reattached and the
	// app state must survive.
	f, diag := syntax.Parse([]byte(`Window {
		Column {
			spacing: 4
			Text { id: label text: "0" }
			Button { id: inc width: 100 height: 40 label: "+" }
		}
	}`))
	if !diag.Empty() {
		t.Fatal(diag)
	}
	if err := a.applyReload(f); err != nil {
		t.Fatal(err)
	}
	in.tree = win.tree
	layout.New(textMeasurer{}).Layout(win.tree.Root, 480, 320)

	if c.n != 2 {
		t.Fatalf("app state reset by reload: %d", c.n)
	}
	if got := MustByID[*Text](win, "label").Text.Get(); got != "2" {
		t.Fatalf("label after reload = %q, want the Go-set value", got)
	}

	click()
	if c.n != 3 {
		t.Fatalf("handler not reattached after reload: %d", c.n)
	}
	if got := MustByID[*Text](win, "label").Text.Get(); got != "3" {
		t.Fatalf("label after click = %q", got)
	}
}
