package paint

import "testing"

// TestBoxShadowGeometry pins the CSS mapping: the quad is the shape grown by
// blur+spread so the shader can rebuild the shape, and the spread moves into the
// radius the shader subtracts the blur from.
func TestBoxShadowGeometry(t *testing.T) {
	l := list(t, `Window {
		Rectangle {
			width: 100 height: 50 radius: 8 background: #ff0000
			shadow: 10 10 5 12 #000000
		}
	}`, 400, 400)

	var shadow *Op
	for i := range l.Ops {
		if l.Ops[i].Kind == Shadow {
			shadow = &l.Ops[i]
		}
	}
	if shadow == nil {
		t.Fatalf("no shadow op:\n%s", l.Dump())
	}
	// The rectangle sits at 0,0: grown by 5+12 on every side, moved by 10,10.
	if want := [4]float32{-7, -7, 134, 84}; shadow.Rect != want {
		t.Fatalf("rect = %v, want %v", shadow.Rect, want)
	}
	// radius + spread + blur; the shader subtracts the blur again.
	if want := float32(8 + 12 + 5); shadow.Radius[0] != want {
		t.Fatalf("radius = %v, want %v", shadow.Radius[0], want)
	}
	// The falloff is the blur, not the grown quad.
	if shadow.Blur != 5 {
		t.Fatalf("blur = %v, want 5", shadow.Blur)
	}
	if shadow.Inset {
		t.Fatal("an outer shadow was marked inset")
	}
}

// TestInsetShadowKeepsTheBox guards the other half: an inset shadow paints into
// the shape, so its quad is the element's own box and the offset and spread
// travel separately.
func TestInsetShadowKeepsTheBox(t *testing.T) {
	l := list(t, `Window {
		Rectangle {
			width: 100 height: 50 radius: 8 background: #ff0000
			shadow: 4 6 5 3 #000000 inset
		}
	}`, 400, 400)

	var shadow *Op
	for i := range l.Ops {
		if l.Ops[i].Kind == Shadow {
			shadow = &l.Ops[i]
		}
	}
	if shadow == nil {
		t.Fatalf("no shadow op:\n%s", l.Dump())
	}
	if !shadow.Inset {
		t.Fatal("the inset layer drew as an outer shadow")
	}
	if want := [4]float32{0, 0, 100, 50}; shadow.Rect != want {
		t.Fatalf("rect = %v, want the element box %v", shadow.Rect, want)
	}
	if shadow.OffsetX != 4 || shadow.OffsetY != 6 || shadow.Spread != 3 {
		t.Fatalf("offset/spread = %v/%v/%v", shadow.OffsetX, shadow.OffsetY, shadow.Spread)
	}
	if shadow.Radius[0] != 8 {
		t.Fatalf("radius = %v, want 8", shadow.Radius[0])
	}
}

// TestTextShadowStaysOnTheGlyphs is the rule the property borrows from CSS: on
// Text the shadow copies the glyph run instead of drawing a box behind the node.
func TestTextShadowStaysOnTheGlyphs(t *testing.T) {
	l := list(t, `Window {
		Text {
			text: "hi"
			fontSize: 20
			shadow: 2 2 3 #ff0000, -1 -1 #0000ff
			background: #ffff00
		}
	}`, 400, 400)

	for _, op := range l.Ops {
		if op.Kind == Shadow {
			t.Fatalf("a Text drew a box shadow:\n%s", l.Dump())
		}
	}
	var text *Op
	for i := range l.Ops {
		if l.Ops[i].Kind == Text {
			text = &l.Ops[i]
		}
	}
	if text == nil {
		t.Fatalf("no text op:\n%s", l.Dump())
	}
	if len(text.TextShadows) != 2 {
		t.Fatalf("text shadows = %+v", text.TextShadows)
	}
	if text.TextShadows[0].Color != 0xFF0000FF || text.TextShadows[1].Color != 0x0000FFFF {
		t.Fatalf("shadow colours = %+v", text.TextShadows)
	}
	if text.TextShadows[1].X != -1 {
		t.Fatalf("second offset = %v, want -1", text.TextShadows[1].X)
	}
}

// TestInsetIsDroppedFromText keeps a `… inset` layer out of a text run: CSS
// text-shadow has no inset.
func TestInsetIsDroppedFromText(t *testing.T) {
	l := list(t, `Window {
		Text { text: "hi" shadow: 2 2 3 #ff0000 inset }
	}`, 400, 400)
	for _, op := range l.Ops {
		if op.Kind == Text && len(op.TextShadows) != 0 {
			t.Fatalf("an inset layer reached the glyph run: %+v", op.TextShadows)
		}
	}
}

// TestShadowFollowsComponentShape checks that a component without an explicit
// radius still gets a matching shadow: a Button is a pill, a Card keeps its M3
// corners.
func TestShadowFollowsComponentShape(t *testing.T) {
	l := list(t, `Window {
		Column {
			Button { label: "x" shadow: 2 2 }
			Card { label: "y" shadow: 2 2 }
		}
	}`, 400, 400)
	var radii []float32
	for _, op := range l.Ops {
		if op.Kind == Shadow {
			radii = append(radii, op.Radius[0])
		}
	}
	if len(radii) == 0 {
		t.Fatalf("no shadows:\n%s", l.Dump())
	}
	// The button's pill radius is half its height, so radius-blur equals the
	// height; the card's is the M3 medium shape.
	if radii[0] <= float32(12) {
		t.Fatalf("button shadow radius = %v, want a pill", radii[0])
	}
}
