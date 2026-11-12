package tree

import "testing"

func buildModifier(t *testing.T, src string) *Tree {
	t.Helper()
	tr, diag := BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	return tr
}

// TestConditionalPicksFirstTrueArm pins the whole point of the modifier: the
// first matching arm wins and the later ones are never consulted, and a pass
// with no match leaves the property where it was.
func TestConditionalPicksFirstTrueArm(t *testing.T) {
	tr := buildModifier(t, `Window {
		Column {
			Text {
				id:   label
				text: conditional { "wide": window.width > 500 "narrow": window.width > 300 }
			}
		}
	}`)
	n, ok := tr.ByID("label")
	if !ok {
		t.Fatal("no label")
	}
	label := n.(*Text)

	read := func(w float64) string {
		tr.SetViewport(w, 600)
		tr.ApplyExpressions()
		return label.Text.Get()
	}
	if got := read(800); got != "wide" {
		t.Fatalf("at 800 text = %q, want the first arm", got)
	}
	if got := read(400); got != "narrow" {
		t.Fatalf("at 400 text = %q, want the second arm", got)
	}
	// Neither arm matches, so the value the last pass wrote stays.
	if got := read(200); got != "narrow" {
		t.Fatalf("at 200 text = %q, want the previous value", got)
	}
}

// TestResponsiveModifierMatchesMap checks that the keyword form is the same
// registration as the old brace map.
func TestResponsiveModifierMatchesMap(t *testing.T) {
	tr := buildModifier(t, `Window {
		Column {
			id:      box
			padding: responsive { base: 8 md: 16 }
		}
	}`)
	box, _ := tr.ByID("box")
	tr.ApplyBreakpoints(1000)
	if got := box.Base().Padding(); got != 16 {
		t.Fatalf("padding = %v, want 16", got)
	}
	tr.ApplyBreakpoints(300)
	if got := box.Base().Padding(); got != 8 {
		t.Fatalf("padding = %v, want the base 8", got)
	}
}

// TestPaddingAxisShorthands covers paddingX/paddingY in a real build: an axis
// sets both of its edges and a single edge still wins, so a design can set a
// vertical rhythm once and override one side.
func TestPaddingAxisShorthands(t *testing.T) {
	tr := buildResponsive(t, `Window {
		Column {
			id: box
			padding: 4
			paddingX: 16
			paddingY: 2
			paddingLeft: 30
		}
	}`)
	box, _ := tr.ByID("box")
	want := Edges{Top: 2, Right: 16, Bottom: 2, Left: 30}
	if got := box.Base().PaddingEdges(); got != want {
		t.Fatalf("padding edges = %+v, want %+v", got, want)
	}
	// An axis left out follows padding, which is what a design that only wrote
	// paddingX expects on the vertical sides.
	tr = buildResponsive(t, `Window {
		Column {
			id: box
			padding: 6
			paddingX: 20
		}
	}`)
	box, _ = tr.ByID("box")
	if got := box.Base().PaddingEdges(); got != (Edges{Top: 6, Right: 20, Bottom: 6, Left: 20}) {
		t.Fatalf("paddingX alone = %+v", got)
	}
}
