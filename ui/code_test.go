package ui

import (
	"testing"

	"github.com/ksckaan1/goi/internal/paint"
	"github.com/ksckaan1/goi/internal/tree"
)

const codeUI = `Window {
	Column {
		Code {
			id: block
			language: go
			showCopy: true
			text: "func main() {\n\tfmt.Println(42)\n}\n"
			width: 400
			height: 200
		}
	}
}`

// Dragging across a code block selects the text under the pointer, and Ctrl+C
// copies exactly that slice.
func TestCodeIsSelectable(t *testing.T) {
	_, win, in := inputFixture(t, codeUI, 500, 300)
	code := MustByID[*Code](win, "block")
	box := paint.CodeBox(absoluteRect(code))

	in.MouseDown(box.X+2, box.Y+2, 0, false)
	in.MouseMove(box.X+120, box.Y+2)
	in.MouseUp(box.X+120, box.Y+2, 0)

	a, b := code.SelA, code.SelB
	if a == b {
		t.Fatal("dragging across a Code block selected nothing")
	}
	var got string
	in.setClipboard = func(s string) { got = s }
	in.key(tree.KeyC, tree.Mods{Ctrl: true})
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	if got == "" || got != code.Source()[lo:hi] {
		t.Fatalf("copied %q, want %q", got, code.Source()[lo:hi])
	}
	if len(got) > len("func main() {") {
		t.Fatalf("selection ran past the first line: %q", got)
	}
}

// A click on the copy button copies the whole block and does not start a
// selection drag.
func TestCodeCopyButton(t *testing.T) {
	_, win, in := inputFixture(t, codeUI, 500, 300)
	code := MustByID[*Code](win, "block")
	var got string
	in.setClipboard = func(s string) { got = s }

	btn := paint.CodeCopyRect(absoluteRect(code))
	in.MouseDown(btn.X+btn.W/2, btn.Y+btn.H/2, 0, false)
	in.MouseUp(btn.X+btn.W/2, btn.Y+btn.H/2, 0)

	if got != code.Source() {
		t.Fatalf("copied %q, want the whole block", got)
	}
	if code.SelA != code.SelB {
		t.Fatalf("the copy press started a selection: %d..%d", code.SelA, code.SelB)
	}
}

// Without showCopy there is no button, so a press in that corner selects text
// like anywhere else.
func TestCodeCopyButtonIsOptIn(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column { Code { id: block text: "x = 1" width: 400 height: 120 } }
	}`, 500, 200)
	code := MustByID[*Code](win, "block")
	copied := false
	in.setClipboard = func(string) { copied = true }

	btn := paint.CodeCopyRect(absoluteRect(code))
	in.MouseDown(btn.X+btn.W/2, btn.Y+btn.H/2, 0, false)
	in.MouseUp(btn.X+btn.W/2, btn.Y+btn.H/2, 0)
	if copied {
		t.Fatal("copied with showCopy off")
	}
}

// A double click selects a word inside a code block, the same as in prose.
func TestCodeDoubleClickSelectsWord(t *testing.T) {
	_, win, in := inputFixture(t, codeUI, 500, 300)
	code := MustByID[*Code](win, "block")
	box := paint.CodeBox(absoluteRect(code))
	st := paint.CodeStyle()
	x := box.X + in.measureStyled("func ma", st) // inside "main"
	y := box.Y + st.LineHeight/2

	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if got := code.Source()[code.SelA:code.SelB]; got != "main" {
		t.Fatalf("double click selected %q, want %q", got, "main")
	}
}

// The highlight is cached on the node: painting twice without touching the
// props must not re-lex, and changing either prop must.
func TestCodeHighlightCache(t *testing.T) {
	_, win, _ := inputFixture(t, codeUI, 500, 300)
	code := MustByID[*Code](win, "block")

	first := code.Spans()
	if len(first) == 0 {
		t.Fatal("no spans")
	}
	if got := code.Spans(); &got[0] != &first[0] {
		t.Fatal("spans were recomputed without a change")
	}
	code.Language.Set("python")
	python := code.Spans()
	if len(python) == 0 || &python[0] == &first[0] {
		t.Fatal("changing the language did not re-lex")
	}
}

// A ClipRRect narrows the clip of everything it draws and rounds its corners.
func TestClipRRectClipsChildren(t *testing.T) {
	_, win, _ := inputFixture(t, `Window {
		ClipRRect {
			id: clip
			radius: 16
			width: 100
			height: 100
			Rectangle { width: 300 height: 300 background: #FF0000FF }
		}
	}`, 200, 200)
	clip := MustByID[*ClipRRect](win, "clip")
	r := absoluteRect(clip)

	list := &paint.List{}
	list.Build(win.tree, 200, 200)

	found := false
	for _, op := range list.Ops {
		if op.Kind != paint.Fill {
			continue
		}
		// The oversized rectangle must be cut to the ClipRRect's box...
		if op.Clip[2]-op.Clip[0] > float32(r.W)+1 || op.Clip[3]-op.Clip[1] > float32(r.H)+1 {
			t.Fatalf("child clip %v is wider than the ClipRRect %v", op.Clip, r)
		}
		// ...with rounded corners.
		if op.ClipRadius != 16 {
			t.Fatalf("clip radius = %v, want 16", op.ClipRadius)
		}
		found = true
	}
	if !found {
		t.Fatal("no child op was painted")
	}
}

// Without a radius a ClipRRect only narrows the clip, which still keeps a
// shadow or a scrolled row inside its box.
func TestClipRRectZeroRadius(t *testing.T) {
	_, win, _ := inputFixture(t, `Window {
		ClipRRect {
			width: 100
			height: 100
			Rectangle { width: 300 height: 300 background: #FF0000FF }
		}
	}`, 200, 200)
	list := &paint.List{}
	list.Build(win.tree, 200, 200)
	for _, op := range list.Ops {
		if op.Clip[2]-op.Clip[0] > 101 {
			t.Fatalf("clip %v not narrowed", op.Clip)
		}
		if op.ClipRadius != 0 {
			t.Fatalf("clip radius = %v, want none", op.ClipRadius)
		}
	}
}
