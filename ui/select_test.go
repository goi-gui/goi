package ui

import (
	"testing"

	"github.com/ksckaan1/goui/internal/platform"
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

// TestSelectableTextDragsAcrossLines covers the wrapped case: a press on the
// second line anchors there, and dragging to the last line selects the range in
// between rather than restarting on line one.
func TestSelectableTextDragsAcrossLines(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			padding: 10
			Text {
				id: body
				text: "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau upsilon"
				wrap: true
				selectable: true
				width: 160
			}
		}
	}`, 300, 300)
	body := MustByID[*Text](win, "body")
	box := absoluteRect(body)
	st := textStyle(body)
	lines := in.textLines(body, box.W)
	if len(lines) < 3 {
		t.Fatalf("fixture wrapped into %d lines, want at least 3", len(lines))
	}
	lh := st.LineHeight
	if lh <= 0 {
		lh = st.Size * 1.2
	}
	in.MouseDown(box.X+2, box.Y+lh*1.5, 0, false)
	if body.SelA != lines[1].Start {
		t.Fatalf("press on line 2 anchored at %d, want %d", body.SelA, lines[1].Start)
	}
	last := len(lines) - 1
	in.MouseMove(box.X+box.W-2, box.Y+lh*(float64(last)+0.5))
	in.MouseUp(box.X+box.W-2, box.Y+lh*(float64(last)+0.5), 0)
	want := body.Text.Get()[lines[1].Start:lines[last].End]
	var got string
	in.setClipboard = func(s string) { got = s }
	in.key(tree.KeyC, tree.Mods{Ctrl: true})
	if got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

// TestSelectableTextHitFollowsScroll guards the anchor offset: a scrolled
// container moves the glyphs, and the press must map against the moved box.
func TestSelectableTextHitFollowsScroll(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			id: page
			scroll: true
			height: 50
			Text { width: 200 height: 40 }
			Text {
				id: body
				text: "alpha beta gamma delta epsilon zeta eta theta iota kappa"
				wrap: true
				selectable: true
				width: 160
			}
		}
	}`, 300, 300)
	page := MustByID[*Column](win, "page")
	body := MustByID[*Text](win, "body")
	page.Base().SetScrollOffset(40)
	box := absoluteRect(body)
	st := textStyle(body)
	lh := st.LineHeight
	if lh <= 0 {
		lh = st.Size * 1.2
	}
	lines := in.textLines(body, box.W)
	if len(lines) < 2 {
		t.Fatalf("fixture wrapped into %d lines, want at least 2", len(lines))
	}
	if got := in.textIndexAt(body, box.X+1, box.Y+lh*1.5); got != lines[1].Start {
		t.Fatalf("press on line 2 anchored at %d, want %d", got, lines[1].Start)
	}
}

// TestPointerShapeFollowsHover is the M3 affordance: I-beam over text that can
// be edited or selected, pointing hand over anything clickable, arrow elsewhere.
func TestPointerShapeFollowsHover(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			padding: 10
			Button { id: btn width: 100 height: 40 label: "Go" }
			TextField { id: field width: 200 height: 30 }
			Text { id: body text: "hello world" selectable: true width: 200 height: 20 }
			Text { id: plain text: "plain label" width: 200 height: 20 }
		}
	}`, 480, 320)
	var got platform.Cursor
	in.setCursor = func(c platform.Cursor) { got = c }
	for _, tc := range []struct {
		id   string
		want platform.Cursor
	}{
		{"btn", platform.CursorPointer},
		{"field", platform.CursorText},
		{"body", platform.CursorText},
		{"plain", platform.CursorArrow},
	} {
		n, ok := win.tree.ByID(tc.id)
		if !ok {
			t.Fatalf("no node %q", tc.id)
		}
		r := absoluteRect(n)
		got = -1
		in.MouseMove(r.X+r.W/2, r.Y+r.H/2)
		if got != tc.want {
			t.Errorf("hover on %s gave cursor %v, want %v", tc.id, got, tc.want)
		}
	}
}
