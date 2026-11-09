package ui

import (
	"os"
	"testing"

	"github.com/goi-gui/goi/internal/asset"
	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/platform"
	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/tree"
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

// Double click selects the word under the pointer and triple click the whole
// paragraph, without needing a drag.
func TestSelectableTextWordAndParagraph(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			Text {
				id: body
				text: "first line\nsecond line here\nthird"
				selectable: true
				wrap: true
				width: 200
				height: 60
			}
		}
	}`, 300, 120)
	body := MustByID[*Text](win, "body")
	b := absoluteRect(body)

	// Put the pointer inside "line" on the second line.
	x := b.X + in.measureStyled("second li", textStyle(body))
	y := b.Y + textStyle(body).LineHeight*1.5
	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if got := body.Text.Get()[body.SelA:body.SelB]; got != "" {
		t.Fatalf("first click selected %q, want just a caret", got)
	}

	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if got := body.Text.Get()[body.SelA:body.SelB]; got != "line" {
		t.Fatalf("double click selected %q, want %q", got, "line")
	}

	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if got := body.Text.Get()[body.SelA:body.SelB]; got != "second line here" {
		t.Fatalf("triple click selected %q, want the paragraph", got)
	}
}

// TestCursorPropertyOverridesAffordance pins the CSS rule: a declared cursor
// wins over the M3 shape, one declaration covers the whole subtree, and an
// explicit auto resets it.
func TestCursorPropertyOverridesAffordance(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Column {
			cursor: pointer
			Button { id: btn width: 100 height: 40 label: "Go" }
			TextField { id: field width: 200 height: 30 }
			Column {
				id: inbox
				cursor: auto
				Text { id: plain text: "plain" width: 100 height: 20 }
			}
		}
	}`, 480, 320)
	var got platform.Cursor
	in.setCursor = func(c platform.Cursor) { got = c }
	check := func(id string, want platform.Cursor) {
		t.Helper()
		n, ok := win.tree.ByID(id)
		if !ok {
			t.Fatalf("no node %q", id)
		}
		r := absoluteRect(n)
		got = -1
		in.MouseMove(r.X+r.W/2, r.Y+r.H/2)
		if got != want {
			t.Errorf("cursor over %s = %v, want %v", id, got, want)
		}
	}
	// Inherited from the column, including over a text field.
	check("btn", platform.CursorPointer)
	check("field", platform.CursorPointer)
	// The inner column says auto, so the framework decides again.
	check("plain", platform.CursorArrow)
}

// TestCursorForMapsEveryCSSValue pins the mapping table: every CSS name resolves
// to a shape, and the ones SDL has no cursor for fall back to the arrow rather
// than leaving the previous shape up.
func TestCursorForMapsEveryCSSValue(t *testing.T) {
	want := map[string]platform.Cursor{
		"auto":          -1, // not a shape: the framework decides
		"default":       platform.CursorArrow,
		"none":          platform.CursorNone,
		"context-menu":  platform.CursorArrow,
		"help":          platform.CursorArrow,
		"pointer":       platform.CursorPointer,
		"progress":      platform.CursorProgress,
		"wait":          platform.CursorWait,
		"cell":          platform.CursorCrosshair,
		"crosshair":     platform.CursorCrosshair,
		"text":          platform.CursorText,
		"vertical-text": platform.CursorText,
		"alias":         platform.CursorArrow,
		"copy":          platform.CursorArrow,
		"move":          platform.CursorMove,
		"no-drop":       platform.CursorNotAllowed,
		"not-allowed":   platform.CursorNotAllowed,
		"grab":          platform.CursorMove,
		"grabbing":      platform.CursorMove,
		"all-scroll":    platform.CursorMove,
		"col-resize":    platform.CursorResizeEW,
		"ew-resize":     platform.CursorResizeEW,
		"row-resize":    platform.CursorResizeNS,
		"ns-resize":     platform.CursorResizeNS,
		"n-resize":      platform.CursorResizeN,
		"e-resize":      platform.CursorResizeE,
		"s-resize":      platform.CursorResizeS,
		"w-resize":      platform.CursorResizeW,
		"ne-resize":     platform.CursorResizeNE,
		"nw-resize":     platform.CursorResizeNW,
		"se-resize":     platform.CursorResizeSE,
		"sw-resize":     platform.CursorResizeSW,
		"nesw-resize":   platform.CursorResizeNESW,
		"nwse-resize":   platform.CursorResizeNWSE,
		"zoom-in":       platform.CursorArrow,
		"zoom-out":      platform.CursorArrow,
	}
	for _, name := range schema.CursorValues {
		expect, ok := want[name]
		if !ok {
			t.Fatalf("%q has no expectation; add it here when CursorValues grows", name)
		}
		_, win, in := inputFixture(t, `Window { Rectangle { id: box width: 50 height: 50 cursor: `+name+` } }`, 100, 100)
		box, _ := win.tree.ByID("box")
		r := absoluteRect(box)
		got := platform.Cursor(-2)
		in.setCursor = func(c platform.Cursor) { got = c }
		in.MouseMove(r.X+r.W/2, r.Y+r.H/2)
		if expect < 0 {
			if got == platform.CursorNone || got == platform.CursorText || got == platform.CursorPointer {
				t.Errorf("cursor %q took a shape instead of auto", name)
			}
			continue
		}
		if got != expect {
			t.Errorf("cursor %q gave %v, want %v", name, got, expect)
		}
	}
}

// TestCursorExampleTilesResolve is the end-to-end guard for the cursor example:
// every tile declares a value, the hit test lands on it, and the pointer shape
// changes. Tiles are laid out in one tall viewport so all of them are reachable
// without scrolling. The examples live in the sibling goi-gui/examples
// repository, so CI without that checkout skips the test.
func TestCursorExampleTilesResolve(t *testing.T) {
	dir := "../../examples/cursor"
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			t.Skip("examples are not checked out next to the repo")
		}
		t.Fatal(err)
	}
	b, diag := asset.Load(os.DirFS(dir), "cursor.goi")
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr, err := tree.Build(b.File, nil)
	if err != nil {
		t.Fatal(err)
	}
	layout.New(textMeasurer{}).Layout(tr.Root, 760, 2400)
	in := &interactor{tree: tr, viewportW: 760, viewportH: 2400}
	seen, checked := 0, 0
	for _, n := range tr.All() {
		decl := n.Base().Cursor()
		if decl == "" {
			continue
		}
		seen++
		r := absoluteRect(n)
		hit := in.hit(r.X+r.W/2, r.Y+r.H/2)
		if hit == nil {
			t.Fatalf("%s tile %q is not hit-testable at %v", n.Kind(), decl, r)
		}
		// A container's centre can land on a child that declares its own value,
		// which is the CSS result too; leave that case to the inheritance test.
		if hit != n {
			continue
		}
		c, ok := cursorFor(hit)
		if decl == "auto" {
			if ok {
				t.Fatalf("cursor: auto resolved to %v, want the framework's shape", c)
			}
			continue
		}
		if !ok {
			t.Fatalf("%s tile %q resolved to no cursor", n.Kind(), decl)
		}
		checked++
	}
	if seen < 30 || checked < 30 {
		t.Fatalf("%d declarations, %d checked; want the whole example", seen, checked)
	}
}
