package ui

import (
	"testing"

	"github.com/ksckaan1/goi/internal/paint"
	"github.com/ksckaan1/goi/internal/tree"
)

// focusText clicks a field so it owns the keyboard, the way a user would.
func focusText(t *testing.T, in *interactor, n tree.Node) {
	t.Helper()
	b := absoluteRect(n)
	in.MouseDown(b.X+20, b.Y+45, 0, false)
	in.MouseUp(b.X+20, b.Y+45, 0)
	if in.focus == nil {
		t.Fatalf("%s did not take focus", n.Kind())
	}
}

const textAreaUI = `Window {
	Column {
		TextArea {
			id: area
			label: "Notes"
			text: "alpha beta\ngamma delta"
			width: 300
			height: 140
		}
	}
}`

// Enter inserts a newline instead of submitting, and the value keeps both lines.
func TestTextAreaEnterAddsLine(t *testing.T) {
	_, win, in := inputFixture(t, textAreaUI, 400, 240)
	a := MustByID[*TextArea](win, "area")

	var submitted, edited int
	a.OnSubmit(func(string) { submitted++ })
	a.OnEdit(func(string) { edited++ })

	// Click at the end of the first line, then press Enter: the newline lands
	// there, not at the end of the value.
	in.MouseDown(absoluteRect(a).X+20, absoluteRect(a).Y+45, 0, false)
	in.MouseUp(absoluteRect(a).X+20, absoluteRect(a).Y+45, 0)
	in.key(tree.KeyEnd, tree.Mods{})
	in.key(tree.KeyEnter, tree.Mods{})

	if submitted != 0 {
		t.Fatalf("Enter submitted a multiline field")
	}
	if edited == 0 {
		t.Fatalf("Enter did not report an edit")
	}
	if got := a.Text.Get(); got != "alpha beta\n\ngamma delta" {
		t.Fatalf("value = %q, want a new line after the first", got)
	}
}

// A single-line TextField still submits on Enter, which is the contrast case.
func TestTextFieldEnterStillSubmits(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column { TextField { id: f text: "x" width: 200 height: 56 } }
	}`, 300, 120)
	f := MustByID[*TextField](win, "f")
	var submitted int
	f.OnSubmit(func(string) { submitted++ })
	b := absoluteRect(f)
	in.MouseDown(b.X+20, b.Y+28, 0, false)
	in.MouseUp(b.X+20, b.Y+28, 0)
	in.key(tree.KeyEnter, tree.Mods{})
	if submitted != 1 {
		t.Fatalf("submitted %d times, want 1", submitted)
	}
	if got := f.Text.Get(); got != "x" {
		t.Fatalf("Enter changed the value to %q", got)
	}
}

// Clicking a line in a multiline field puts the caret on that line, not at the
// same byte offset of the first one.
func TestTextAreaClickPicksLine(t *testing.T) {
	_, win, in := inputFixture(t, textAreaUI, 400, 240)
	a := MustByID[*TextArea](win, "area")
	box := paint.TextAreaBox(absoluteRect(a))
	lh := textAreaLineHeight()

	// Click at the very start of the second line.
	in.MouseDown(box.X+1, box.Y+lh*1.5, 0, false)
	in.MouseUp(box.X+1, box.Y+lh*1.5, 0)
	first := a.Caret

	in.MouseDown(box.X+1, box.Y+0.5*lh, 0, false)
	in.MouseUp(box.X+1, box.Y+0.5*lh, 0)
	second := a.Caret

	if second == first {
		t.Fatalf("both lines mapped to caret %d", first)
	}
	if a.Text.Get()[second:first] != "alpha beta\n" {
		t.Fatalf("carets %d and %d do not bracket the first line", second, first)
	}
}

// Home and End work per line in a multiline field.
func TestTextAreaHomeEndPerLine(t *testing.T) {
	_, win, in := inputFixture(t, textAreaUI, 400, 240)
	a := MustByID[*TextArea](win, "area")
	focusText(t, in, a)
	s := a.Text.Get()
	a.Caret = len("alpha beta\ngam")

	in.key(tree.KeyHome, tree.Mods{})
	if got := a.Caret; s[got:] != "gamma delta" {
		t.Fatalf("Home put the caret at %d, want the start of line 2", got)
	}
	in.key(tree.KeyEnd, tree.Mods{})
	if got := a.Caret; got != len(s) {
		t.Fatalf("End put the caret at %d, want %d", got, len(s))
	}
}

// Down moves to the wrapped line below, keeping the column.
func TestTextAreaArrowDown(t *testing.T) {
	_, win, in := inputFixture(t, textAreaUI, 400, 240)
	a := MustByID[*TextArea](win, "area")
	focusText(t, in, a)
	a.Caret = len("alpha b")
	in.key(tree.KeyDown, tree.Mods{})
	if s := a.Text.Get(); a.Caret <= len("alpha beta\n") {
		t.Fatalf("Down stayed on line 1 (caret %d of %q)", a.Caret, s)
	}
	in.key(tree.KeyUp, tree.Mods{})
	if a.Caret != len("alpha b") {
		t.Fatalf("Up returned to %d, want %d", a.Caret, len("alpha b"))
	}
}
