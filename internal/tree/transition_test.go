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

// TestResponsiveTransitionDoesNotHopOnFirstValue is the shopping gallery: a
// responsive breakpoint picks the first expression target. The first target is
// written directly, and asking for the same target again does not restart it.
func TestResponsiveTransitionDoesNotHopOnFirstValue(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { selected int }
Window {
	Rectangle {
		id:     box
		height: responsive {
			base: 400
			sm: bridge.selected == 0 ? 500 : 200
		}
		transition: height 500ms
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	tr.SetViewport(1000, 800)
	tr.ApplyBreakpoints(1000)
	tr.ApplyExpressions()
	if box.Height() != 500 {
		t.Fatalf("first height = %v, want 500", box.Height())
	}
	tr.AdvanceTransitions(0.1)
	if box.Height() != 500 {
		t.Fatalf("height hopped to %v after the first value", box.Height())
	}

	tr.SetData("selected", 0)
	tr.ApplyExpressions()
	tr.AdvanceTransitions(0.1)
	if box.Height() != 500 {
		t.Fatalf("same selection hopped to %v", box.Height())
	}

	tr.SetData("selected", 1)
	tr.ApplyExpressions()
	tr.AdvanceTransitions(0.1)
	if h := box.Height(); h <= 200 || h >= 500 {
		t.Fatalf("mid height = %v, want a value between 200 and 500", h)
	}
	tr.AdvanceTransitions(0.5)
	if box.Height() != 200 {
		t.Fatalf("final height = %v, want 200", box.Height())
	}
}

// TestTransitionSurvivesExpressionSetData is the gallery image: a selected
// index used only in an expression updates the existing rows, so their
// transition state survives and the heights animate instead of resetting.
func TestTransitionSurvivesExpressionSetData(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { items []string selected int }
Window {
	Column {
		for j, path := range bridge.items {
			Text {
				text:       path
				height:     bridge.selected == j ? 500 : 200
				transition: height 500ms
			}
		}
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetData("items", []string{"a", "b"})
	tr.SetData("selected", 0)
	tr.ApplyExpressions()
	before := tr.All()
	tr.SetData("selected", 1)
	tr.ApplyExpressions()
	after := tr.All()
	if len(before) != len(after) {
		t.Fatalf("rows rebuilt: %d -> %d nodes", len(before), len(after))
	}
	texts := []*Text{}
	for _, n := range after {
		if tx, ok := n.(*Text); ok {
			texts = append(texts, tx)
		}
	}
	if len(texts) != 2 || texts[0].Height() != 500 || texts[1].Height() != 200 {
		t.Fatalf("heights jumped to %v and %v", texts[0].Height(), texts[1].Height())
	}
	tr.AdvanceTransitions(0.1)
	if texts[0].Height() <= 200 || texts[0].Height() >= 500 ||
		texts[1].Height() <= 200 || texts[1].Height() >= 500 {
		t.Fatalf("mid heights = %v and %v, want both moving", texts[0].Height(), texts[1].Height())
	}
}

// TestBoxTransitions covers the other numeric and colour properties: padding,
// margin, border thickness and border colour use the same cells.
func TestBoxTransitions(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { open bool }
Window {
	Rectangle {
		id:              box
		padding:         bridge.open ? 20 : 4
		margin:          bridge.open ? 16 : 2
		borderThickness: bridge.open ? 4 : 1
		borderColor:     bridge.open ? #ff0000ff : #0000ffff
		transition: padding 500ms, margin 500ms, borderThickness 500ms, borderColor 500ms
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	tr.ApplyExpressions()
	if box.Padding() != 4 || box.Margin() != 2 || box.BorderThickness() != 1 || box.BorderColor() != 0x0000FFFF {
		t.Fatalf("initial box = %v %v %v %08x", box.Padding(), box.Margin(), box.BorderThickness(), box.BorderColor())
	}
	tr.SetData("open", true)
	tr.ApplyExpressions()
	if box.Padding() != 4 || box.Margin() != 2 {
		t.Fatalf("box jumped before the first frame: %v %v", box.Padding(), box.Margin())
	}
	tr.AdvanceTransitions(0.25)
	if p := box.Padding(); p <= 4 || p >= 20 {
		t.Fatalf("padding mid = %v", p)
	}
	if m := box.Margin(); m <= 2 || m >= 16 {
		t.Fatalf("margin mid = %v", m)
	}
	if b := box.BorderThickness(); b <= 1 || b >= 4 {
		t.Fatalf("border mid = %v", b)
	}
	if c := box.BorderColor(); c == 0x0000FFFF || c == 0xFF0000FF {
		t.Fatalf("border colour mid = %08x", c)
	}
	tr.AdvanceTransitions(0.25)
	if box.Padding() != 20 || box.Margin() != 16 || box.BorderThickness() != 4 || box.BorderColor() != 0xFF0000FF {
		t.Fatalf("final box = %v %v %v %08x", box.Padding(), box.Margin(), box.BorderThickness(), box.BorderColor())
	}
}

// TestGoSetterTransition keeps design transitions working when Go writes a
// property through its setter.
func TestGoSetterTransition(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
	Rectangle {
		id:         box
		width:      100
		background: #0000ffff
		transition: width 500ms, background 500ms
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].(*Rectangle)
	box.SetWidth(500)
	box.SetBackground(0xff0000ff)
	if box.Width() != 100 || box.Background() != 0x0000FFFF {
		t.Fatalf("setters jumped to %v and %08x", box.Width(), box.Background())
	}
	tr.AdvanceTransitions(0.25)
	if w := box.Width(); w <= 100 || w >= 500 {
		t.Fatalf("width mid = %v", w)
	}
	if c := box.Background(); c == 0x0000FFFF || c == 0xff0000ff {
		t.Fatalf("background mid = %08x", c)
	}
	tr.AdvanceTransitions(0.25)
	if box.Width() != 500 || box.Background() != 0xff0000ff {
		t.Fatalf("final = %v and %08x", box.Width(), box.Background())
	}
}
