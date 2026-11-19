package tree

import "testing"

// TestNumberTransition animates a width expression from the old value to the
// new one, then lands exactly on the target.
func TestNumberTransition(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { collapsed bool }
Window {
	Rectangle {
		id:     box
		width:  bridge.collapsed ? 200 : 500
		height: bridge.collapsed ? 150 : 400
		transition: width 500ms, height 500ms
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	tr.ApplyExpressions()
	if box.Width() != 500 || box.Height() != 400 {
		t.Fatalf("initial size = %v x %v", box.Width(), box.Height())
	}
	tr.SetData("collapsed", true)
	tr.ApplyExpressions()
	if box.Width() != 500 {
		t.Fatalf("width jumped to %v before the first transition frame", box.Width())
	}
	tr.AdvanceTransitions(0.25)
	if w := box.Width(); w <= 200 || w >= 500 {
		t.Fatalf("width mid-transition = %v", w)
	}
	tr.AdvanceTransitions(0.25)
	if box.Width() != 200 || box.Height() != 150 {
		t.Fatalf("final size = %v x %v", box.Width(), box.Height())
	}
}

// TestColorTransition mixes the old and new colour, including the alpha.
func TestColorTransition(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { on bool }
Window {
	Rectangle {
		id:         box
		background: bridge.on ? #ff0000ff : #0000ffff
		transition: background 1s
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	tr.ApplyExpressions()
	if box.Background() != 0x0000FFFF {
		t.Fatalf("initial background = %08x", box.Background())
	}
	tr.SetData("on", true)
	tr.ApplyExpressions()
	tr.AdvanceTransitions(0.5)
	if c := box.Background(); c == 0x0000FFFF || c == 0xFF0000FF {
		t.Fatalf("mid background = %08x, want a mix", c)
	}
	tr.AdvanceTransitions(0.5)
	if box.Background() != 0xFF0000FF {
		t.Fatalf("final background = %08x", box.Background())
	}
}

// TestConditionalColorTransition is the shopping shape: a conditional role or
// literal colour animates, and its fallback animates back too.
func TestConditionalColorTransition(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { on bool }
Window {
	Rectangle {
		id:         box
		background: conditional { #ff0000ff: bridge.on }
		transition: background 200ms
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	tr.ApplyExpressions()
	tr.SetData("on", true)
	tr.ApplyExpressions()
	tr.AdvanceTransitions(0.1)
	if c := box.Background(); c == 0 || c == 0xFF0000FF {
		t.Fatalf("mid background = %08x", c)
	}
	tr.AdvanceTransitions(0.1)
	if box.Background() != 0xFF0000FF {
		t.Fatalf("selected background = %08x", box.Background())
	}
	tr.SetData("on", false)
	tr.ApplyExpressions()
	tr.AdvanceTransitions(0.2)
	if box.Background() != 0 {
		t.Fatalf("fallback background = %08x, want 0", box.Background())
	}
}
