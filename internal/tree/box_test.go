package tree

import "testing"

// TestRadiiSpecificity pins the order the shorthands override each other:
// radius, then a side, then a corner, whatever the file's order.
func TestRadiiSpecificity(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			id: a
			radius: 8
			radiusTop: 24
			radiusBottomLeft: 0
		}
		Column {
			id: b
			radius: 8
			radiusLeft: 4
			radiusTop: 24
		}
		Column {
			id: c
			radiusTopLeft: 30
			radius: 8
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	cases := []struct {
		id   string
		want [4]float64
	}{
		// radius, top on both, bottom-left cleared.
		{"a", [4]float64{24, 24, 8, 0}},
		// The left side wins at both left corners over the top side.
		{"b", [4]float64{4, 24, 8, 4}},
		// A corner wins even when radius comes after it.
		{"c", [4]float64{30, 8, 8, 8}},
	}
	for _, c := range cases {
		n, _ := tr.ByID(c.id)
		if got := n.Base().Radii(); got != c.want {
			t.Errorf("%s radii = %v, want %v", c.id, got, c.want)
		}
	}
	if n, _ := tr.ByID("a"); n.Base().Radius() != 24 {
		t.Errorf("Radius = %v, want the largest corner 24", n.Base().Radius())
	}
}

// TestBorderEdgesAndColors covers the per-side fallback: a side that was not
// written follows the box, and one that was wins.
func TestBorderEdgesAndColors(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			id: a
			borderWidth: 1
			borderColor: #ff0000
			borderTopWidth: 4
			borderBottomColor: #00ff00
		}
		Column {
			id: b
			borderWidth: 2
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	a, _ := tr.ByID("a")
	be := a.Base().BorderEdges()
	if be.Top != 4 || be.Right != 1 || be.Bottom != 1 || be.Left != 1 {
		t.Errorf("edges = %+v, want top 4 and the rest 1", be)
	}
	cols := a.Base().BorderColors()
	if cols[0] != cols[1] || cols[1] != cols[3] || cols[3] != 0xFF0000FF {
		t.Errorf("colors = %08x, want the box colour on top, right and left", cols)
	}
	if cols[2] != 0x00FF00FF {
		t.Errorf("bottom = %08x, want the side colour", cols[2])
	}
	if a.Base().UniformBorder() {
		t.Error("an uneven border reported itself uniform")
	}
	if b, _ := tr.ByID("b"); !b.Base().UniformBorder() {
		t.Error("a plain border is not uniform")
	}
	// Layout insets by the side's own width: 4 on top, 1 elsewhere.
	a.Base().SetBounds(Rect{X: 0, Y: 0, W: 100, H: 100})
	if got := a.Base().ContentBox(a.Base().Bounds()); got.Y != 4 || got.X != 1 || got.H != 95 || got.W != 98 {
		t.Errorf("content box = %+v, want the uneven border honoured", got)
	}
}

// TestFontFamilyInherits checks that a family reaches a descendant and that the
// nearest declaration wins.
func TestFontFamilyInherits(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		fontFamily: "Inter"
		Column {
			Text { id: a text: "one" }
			Text { id: b text: "two" fontFamily: "Mono" }
			Column {
				Text { id: c text: "three" }
			}
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	for id, want := range map[string]string{"a": "Inter", "b": "Mono", "c": "Inter"} {
		n, _ := tr.ByID(id)
		if got := n.Base().FontFamily(); got != want {
			t.Errorf("%s family = %q, want %q", id, got, want)
		}
	}
	// A node above any family falls back to the built-in face.
	if got := tr.Root.Base().FontFamily(); got != "Inter" {
		t.Errorf("window family = %q", got)
	}
}
