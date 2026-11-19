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

// TestRotateScaleShadowTransitions covers the remaining animatable cell types,
// including a Go SetShadow.
func TestRotateScaleShadowTransitions(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
	Rectangle {
		id:         box
		rotate:     0deg
		scale:      1
		shadow:     0 2 4 #0000ffff
		transition: rotate 500ms, scale 500ms, shadow 500ms
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].(*Rectangle)
	box.SetRotation(90)
	box.SetScale(2, 2)
	box.SetShadow(ShadowLayer{X: 4, Y: 8, Blur: 12, Color: 0xff0000ff})
	if box.Rotation().Angle != 0 || box.Scaling().X != 1 || box.Shadow()[0].X != 0 {
		t.Fatalf("setters jumped: %+v %+v %+v", box.Rotation(), box.Scaling(), box.Shadow())
	}
	tr.AdvanceTransitions(0.25)
	if a := box.Rotation().Angle; a <= 0 || a >= 1.57079632679 {
		t.Fatalf("rotation mid = %v", a)
	}
	if sc := box.Scaling().X; sc <= 1 || sc >= 2 {
		t.Fatalf("scale mid = %v", sc)
	}
	if sh := box.Shadow()[0]; sh.X <= 0 || sh.X >= 4 {
		t.Fatalf("shadow mid = %+v", sh)
	}
	tr.AdvanceTransitions(0.25)
	if box.Rotation().Angle != 1.5707963267948966 || box.Scaling().X != 2 {
		t.Fatalf("final rotation/scale = %v %v", box.Rotation().Angle, box.Scaling().X)
	}
	if sh := box.Shadow()[0]; sh.X != 4 || sh.Y != 8 || sh.Blur != 12 || sh.Color != 0xff0000ff {
		t.Fatalf("final shadow = %+v", sh)
	}
}

// TestOpacityTransition covers the cell the gallery's fade uses, through a
// design expression and a Go setter.
func TestOpacityTransition(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { hidden bool }
Window {
	Rectangle {
		id:         box
		opacity:    bridge.hidden ? 0 : 1
		transition: opacity 1s linear
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	tr.ApplyExpressions()
	if box.Opacity() != 1 {
		t.Fatalf("initial opacity = %v", box.Opacity())
	}
	tr.SetData("hidden", true)
	tr.ApplyExpressions()
	tr.AdvanceTransitions(0.5)
	if o := box.Opacity(); o < 0.49 || o > 0.51 {
		t.Fatalf("opacity mid = %v, want 0.5", o)
	}
	tr.AdvanceTransitions(0.5)
	if box.Opacity() != 0 {
		t.Fatalf("final opacity = %v", box.Opacity())
	}

	box.SetOpacity(1)
	tr.AdvanceTransitions(0.5)
	if o := box.Opacity(); o < 0.49 || o > 0.51 {
		t.Fatalf("Go opacity mid = %v, want 0.5", o)
	}
	tr.AdvanceTransitions(0.5)
	if box.Opacity() != 1 {
		t.Fatalf("Go final opacity = %v", box.Opacity())
	}
}

// TestTransitionTimingFunction checks that a linear entry moves halfway at half
// time while an ease-in entry lags the clock.
func TestTransitionTimingFunction(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { small bool }
Window {
	Rectangle {
		id:     linear
		width:  bridge.small ? 200 : 500
		height: 10
		transition: width 1s linear
	}
	Rectangle {
		id:     eased
		width:  bridge.small ? 200 : 500
		height: 10
		transition: width 1s ease-in
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	lin, eased := tr.ids["linear"].Base(), tr.ids["eased"].Base()
	tr.ApplyExpressions()
	tr.SetData("small", true)
	tr.ApplyExpressions()
	tr.AdvanceTransitions(0.5)
	if w := lin.Width(); w < 349.9 || w > 350.1 {
		t.Fatalf("linear width at half time = %v, want 350", w)
	}
	if w := eased.Width(); w <= 350 {
		t.Fatalf("ease-in width at half time = %v, want below 350", w)
	}
	tr.AdvanceTransitions(0.5)
	if lin.Width() != 200 || eased.Width() != 200 {
		t.Fatalf("final widths = %v and %v", lin.Width(), eased.Width())
	}
}

// TestTransitionDelay holds the old value until the delay ends, then runs the
// entry for its own duration.
func TestTransitionDelay(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { small bool }
Window {
	Rectangle {
		id:         box
		width:      bridge.small ? 200 : 500
		height:     10
		transition: width 500ms linear 200ms
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	tr.ApplyExpressions()
	tr.SetData("small", true)
	tr.ApplyExpressions()
	if !tr.AdvanceTransitions(0.1) {
		t.Fatal("the delay should keep asking for frames")
	}
	if box.Width() != 500 {
		t.Fatalf("width moved during the delay: %v", box.Width())
	}
	tr.AdvanceTransitions(0.1)
	if box.Width() != 500 {
		t.Fatalf("width moved at the end of the delay: %v", box.Width())
	}
	tr.AdvanceTransitions(0.25)
	if w := box.Width(); w < 349.9 || w > 350.1 {
		t.Fatalf("width mid-run = %v, want 350", w)
	}
	tr.AdvanceTransitions(0.25)
	if box.Width() != 200 {
		t.Fatalf("final width = %v", box.Width())
	}
}

// TestSizeShorthandTransition writes `size` through the width and height
// transitions, whether the shorthand names them or `size` itself is transitioned.
func TestSizeShorthandTransition(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { big bool }
Window {
	Rectangle {
		id:         box
		size:       bridge.big ? 200 : 100
		transition: size 1s linear
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	tr.ApplyExpressions()
	tr.SetData("big", true)
	tr.ApplyExpressions()
	tr.AdvanceTransitions(0.5)
	if w, h := box.Width(), box.Height(); w < 149.9 || w > 150.1 || h < 149.9 || h > 150.1 {
		t.Fatalf("size mid = %v x %v, want 150 x 150", w, h)
	}
	tr.AdvanceTransitions(0.5)
	if box.Width() != 200 || box.Height() != 200 {
		t.Fatalf("final size = %v x %v", box.Width(), box.Height())
	}
}

// TestSpacingGapAliasTransition lets the transition name either spelling of the
// shared spacing cell.
func TestSpacingGapAliasTransition(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { wide bool }
Window {
	Column {
		id:         box
		spacing:    bridge.wide ? 20 : 0
		transition: gap 1s linear
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].(*Column)
	tr.ApplyExpressions()
	tr.SetData("wide", true)
	tr.ApplyExpressions()
	tr.AdvanceTransitions(0.5)
	if s := box.Spacing.Get(); s < 9.9 || s > 10.1 {
		t.Fatalf("spacing mid = %v, want 10", s)
	}
	tr.AdvanceTransitions(0.5)
	if s := box.Spacing.Get(); s != 20 {
		t.Fatalf("final spacing = %v", s)
	}
}
