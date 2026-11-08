package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/tree"
)

// TestBackgroundGradientPaints checks that a CSS gradient on `background`
// becomes one gradient op with its stops, rather than a solid fill.
func TestBackgroundGradientPaints(t *testing.T) {
	const src = `Window { Column {
		Column {
			width: 200
			height: 100
			background: "linear-gradient(90deg, rgba(42,123,155,1) 0%, rgba(87,199,133,1) 50%, rgba(237,221,83,1) 100%)"
		}
	} }`
	l := list(t, src, 400, 300)
	var g *tree.Gradient
	solid := 0
	for _, op := range l.Ops {
		if op.Gradient != nil {
			g = op.Gradient
		} else if op.Kind == Fill && op.Color != 0 {
			solid++
		}
	}
	if g == nil {
		t.Fatalf("no gradient op:\n%s", l.Dump())
	}
	if g.Kind != tree.LinearKind || g.Angle != 90 {
		t.Fatalf("gradient = %+v", g)
	}
	if len(g.Stops) != 3 {
		t.Fatalf("stops = %d, want 3", len(g.Stops))
	}
	if g.Stops[0].Color != 0x2A7B9BFF || g.Stops[2].Color != 0xEDDD53FF {
		t.Fatalf("stop colours = %+v", g.Stops)
	}
	if solid != 0 {
		t.Fatalf("%d solid fills drawn behind the gradient", solid)
	}
}

// TestGradientKeepsRadiusAndBorder checks the box chrome survives: a gradient
// card still has its corners and its 1dp outline.
func TestGradientKeepsRadiusAndBorder(t *testing.T) {
	const src = `Window { Column {
		Column {
			width: 100
			height: 60
			radius: 12
			borderColor: primary
			borderWidth: 1
			background: "radial-gradient(circle, red 0%, blue 100%)"
		}
	} }`
	l := list(t, src, 400, 300)
	for _, op := range l.Ops {
		if op.Gradient != nil {
			if op.Radius[0] != 12 || op.BorderWidth != 1 {
				t.Fatalf("gradient op lost its chrome: %+v", op)
			}
			if op.Gradient.Kind != tree.RadialKind {
				t.Fatalf("kind = %v, want radial", op.Gradient.Kind)
			}
			return
		}
	}
	t.Fatalf("no gradient op:\n%s", l.Dump())
}

// TestCSSColorNamesAndRGB checks the CSS spellings in a design: a bare keyword,
// rgb(), hsl() and a quoted name all resolve to a colour, not a theme role.
func TestCSSColorNamesAndRGB(t *testing.T) {
	const src = `Window { Column {
		Text { text: "a" color: white }
		Text { text: "b" color: "red" }
		Text { text: "c" color: "rgb(42, 123, 155)" }
		Text { text: "d" color: "hsl(200 40% 50%)" }
		Text { text: "e" color: "rgba(0, 0, 0, 0.5)" }
	} }`
	l := list(t, src, 400, 300)
	want := []uint32{0xFFFFFFFF, 0xFF0000FF, 0x2A7B9BFF, 0x4D90B3FF, 0x00000080}
	got := make([]uint32, 0, len(want))
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "a" || op.Kind == Text && op.Text == "b" ||
			op.Kind == Text && op.Text == "c" || op.Kind == Text && op.Text == "d" ||
			op.Kind == Text && op.Text == "e" {
			got = append(got, op.Color)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d colours, want %d:\n%s", len(got), len(want), l.Dump())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("colour %d = %08x, want %08x", i, got[i], want[i])
		}
	}
}
