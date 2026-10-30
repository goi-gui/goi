package ui

import (
	"testing"

	"github.com/ksckaan1/goui/internal/tree"
)

// TestSelectableTextDragsAndCopies covers the whole path a reader uses: press,
// drag across the line, release, Ctrl+C.
func TestSelectableTextDragsAndCopies(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			Text { id: body text: "hello world" selectable: true width: 200 height: 20 }
		}
	}`, 300, 100)
	body := MustByID[*Text](win, "body")
	b := absoluteRect(body)

	in.MouseDown(b.X+2, b.Y+b.H/2, 0, false)
	in.MouseMove(b.X+b.W-2, b.Y+b.H/2)
	in.MouseUp(b.X+b.W-2, b.Y+b.H/2, 0)
	if body.SelA == body.SelB {
		t.Fatal("dragging across a selectable Text made no selection")
	}
	var got string
	in.setClipboard = func(s string) { got = s }
	in.key(tree.KeyC, tree.Mods{Ctrl: true})
	if got != "hello world" {
		t.Fatalf("copied %q, want the whole line", got)
	}

	// A click elsewhere drops the highlight.
	in.MouseDown(b.X, b.Y-40, 0, false)
	if body.SelA != body.SelB {
		t.Fatal("a click outside left the text selected")
	}
}

// TestPlainTextDoesNotSelect keeps the property honest: without selectable, a
// drag is not a selection.
func TestPlainTextDoesNotSelect(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column { Text { id: body text: "hello" width: 200 height: 20 } }
	}`, 300, 100)
	body := MustByID[*Text](win, "body")
	b := absoluteRect(body)
	in.MouseDown(b.X+2, b.Y+b.H/2, 0, false)
	in.MouseMove(b.X+b.W-2, b.Y+b.H/2)
	in.MouseUp(b.X+b.W-2, b.Y+b.H/2, 0)
	if body.SelA != body.SelB {
		t.Fatal("plain text became selectable")
	}
}

// TestPointerFocusHasNoRing is the M3 rule: only a keyboard focus draws the
// outline. A click still focuses the control so Enter can press it.
func TestPointerFocusHasNoRing(t *testing.T) {
	_, win, in := inputFixture(t, inputUI, 480, 320)
	btn := MustByID[*Button](win, "btn")
	b := absoluteRect(btn)
	// Click inside, away from the label, so the ripple is irrelevant here.
	in.MouseDown(b.X+5, b.Y+5, 0, false)
	c := btn.M3()
	if c.FocusRing {
		t.Fatal("a click drew the keyboard focus ring")
	}
	in.MouseUp(b.X+5, b.Y+5, 0)
	in.key(tree.KeyTab, tree.Mods{})
	if !btn.M3().FocusRing && in.kbd != Node(btn) {
		// Tab may have moved on; the node it landed on must show the ring.
		if c := comp(in.kbd); c == nil || !c.FocusRing {
			t.Fatal("Tab did not draw a focus ring")
		}
	}
}
