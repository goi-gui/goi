package layout_test

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// wrappedHeight is what paint reserves for a wrapped run at width: one op per
// line, each on the role's line height.
func wrappedHeight(s string, width float64, role string) float64 {
	st := m3.RoleStyle(role)
	lines := text.WrapRanges(s, width, func(run string) float64 {
		return float64(len([]rune(run))) * st.Size * 0.55
	})
	return float64(len(lines)) * st.LineHeight
}

const review = "I've been living in this t-shirt since it arrived. As someone who is " +
	"constantly going from video call to meet friends, it's been a lifesaver. " +
	"The cornflower colour is stunning in person."

// TestWrappedTextTakesItsColumnWidth is the shop example's overflow: a wrapped
// run kept its unwrapped width inside a narrow column, so it wrapped for one
// width and painted in another, and the card around it was the wrong height.
func TestWrappedTextTakesItsColumnWidth(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		width: 220
		Text { id: t text: "`+review+`" role: "bodyMedium" wrap: true }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 500, 500)
	n, _ := tr.ByID("t")
	b := n.Base().Bounds()
	if b.W != 220 {
		t.Fatalf("wrapped width = %v, want the column's 220", b.W)
	}
	if want := wrappedHeight(review, b.W, "bodyMedium"); b.H != want {
		t.Fatalf("wrapped height = %v, want %v", b.H, want)
	}
}

// TestWrappedTextInAnImpossibleColumnKeepsLineHeight: when the column has no
// content width left, the run still reserves the line height paint draws with,
// not the measurer's ascent+descent. That shortfall was what let the review
// text run past the card's bottom edge.
func TestWrappedTextInAnImpossibleColumnKeepsLineHeight(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		width: 60
		padding: 40
		Text { id: t text: "`+review+`" role: "bodyMedium" wrap: true }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 500, 500)
	n, _ := tr.ByID("t")
	b := n.Base().Bounds()
	lineHeight := m3.RoleStyle("bodyMedium").LineHeight
	if b.H != lineHeight {
		t.Fatalf("height = %v, want one line of %v", b.H, lineHeight)
	}
}

// TestCardAroundWrappedTextIsTallEnough is the visible symptom: the card's
// height must cover every line its text paints, padding included.
func TestCardAroundWrappedTextIsTallEnough(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		width: 260
		Column {
			id: card
			padding: 10
			Text { id: t text: "`+review+`" role: "bodyMedium" wrap: true }
		}
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 500, 500)
	card, _ := tr.ByID("card")
	txt, _ := tr.ByID("t")
	cb, tb := card.Base().Bounds(), txt.Base().Bounds()
	if tb.H != wrappedHeight(review, tb.W, "bodyMedium") {
		t.Fatalf("text height = %v, want every line", tb.H)
	}
	if cb.H < tb.H+20 {
		t.Fatalf("card height = %v, text %v plus 20dp padding", cb.H, tb.H)
	}
}

// TestMaxLinesReservesOnlyItsLines: a clamp is a height promise too.
func TestMaxLinesReservesOnlyItsLines(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		width: 200
		Text { id: t text: "`+review+`" role: "bodyMedium" maxLines: 2 }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 500, 500)
	n, _ := tr.ByID("t")
	if got := n.Base().Bounds().H; got != 2*m3.RoleStyle("bodyMedium").LineHeight {
		t.Fatalf("maxLines: 2 height = %v, want two lines", got)
	}
}
