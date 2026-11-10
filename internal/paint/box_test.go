package paint

import "testing"

// TestPerCornerRadiusPaints checks that the four corners reach the op: the
// shape shader takes them per corner, so a design with radiusTop or one corner
// has to arrive as an array rather than one number.
func TestPerCornerRadiusPaints(t *testing.T) {
	l := list(t, `Window {
		Rectangle {
			width: 100 height: 40
			background: #112233
			radius: 8
			radiusTop: 20
			radiusBottomLeft: 0
		}
	}`, 200, 100)
	if len(l.Ops) != 1 {
		t.Fatalf("ops = %d:\n%s", len(l.Ops), l.Dump())
	}
	if got, want := l.Ops[0].Radius, [4]float32{20, 20, 8, 0}; got != want {
		t.Fatalf("radius = %v, want %v", got, want)
	}
}

// TestUnevenBorderPaintsFourSides checks the fallback path: when the sides
// disagree the border is four bands, each in its own colour and width, and the
// fill keeps its own rounded shape without a border.
func TestUnevenBorderPaintsFourSides(t *testing.T) {
	l := list(t, `Window {
		Rectangle {
			width: 100 height: 50
			background: #112233
			borderWidth: 2
			borderColor: #ff0000
			borderTopWidth: 6
			borderLeftColor: #00ff00
		}
	}`, 200, 100)
	if len(l.Ops) != 5 {
		t.Fatalf("ops = %d, want a fill and four bands:\n%s", len(l.Ops), l.Dump())
	}
	fill := l.Ops[0]
	if fill.BorderWidth != 0 {
		t.Fatalf("the fill kept a border of %v; the bands draw it", fill.BorderWidth)
	}
	top := l.Ops[1]
	if top.Rect[3] != 6 || top.Color != 0xFF0000FF {
		t.Fatalf("top band = %v colour %08x", top.Rect, top.Color)
	}
	left := l.Ops[2]
	if left.Rect[2] != 2 || left.Color != 0x00FF00FF {
		t.Fatalf("left band = %v colour %08x", left.Rect, left.Color)
	}
	// Every band is cut to the box, so a band cannot paint outside it.
	for i, op := range l.Ops[1:] {
		if op.Clip[0] < 0 || op.Clip[1] < 0 || op.Clip[2] > 100 || op.Clip[3] > 50 {
			t.Fatalf("band %d clip = %v escapes the box", i, op.Clip)
		}
	}
}

// TestUniformBorderStaysOneOp guards the common case: four equal sides are a
// single rounded op, not four bands.
func TestUniformBorderStaysOneOp(t *testing.T) {
	l := list(t, `Window {
		Rectangle {
			width: 100 height: 50
			background: #112233
			borderWidth: 2
			borderColor: #ff0000
			radius: 12
		}
	}`, 200, 100)
	if len(l.Ops) != 1 {
		t.Fatalf("ops = %d, want the one rounded box:\n%s", len(l.Ops), l.Dump())
	}
	if l.Ops[0].BorderWidth != 2 || l.Ops[0].Radius[0] != 12 {
		t.Fatalf("op = %+v", l.Ops[0])
	}
}

// TestFontFamilyReachesTextOps checks that the family a design sets travels
// with the text op, and that a child inherits it.
func TestFontFamilyReachesTextOps(t *testing.T) {
	l := list(t, `Window {
		fontFamily: "Inter"
		Column {
			Text { text: "inherited" }
			Text { text: "own" fontFamily: "Mono" }
		}
	}`, 300, 100)
	got := map[string]string{}
	for _, op := range l.Ops {
		if op.Kind == Text {
			got[op.Text] = op.FontFamily
		}
	}
	if got["inherited"] != "Inter" {
		t.Errorf("inherited family = %q", got["inherited"])
	}
	if got["own"] != "Mono" {
		t.Errorf("own family = %q", got["own"])
	}
}
