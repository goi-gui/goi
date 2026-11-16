package layout_test

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestSetWidthRelaysOut: the Go setters mark the node for layout, so the next
// pass puts the box at the size the app asked for.
func TestSetWidthRelaysOut(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			id:     page
			width:  120
			height: 60
		}
	} `), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	l := layout.New(nil)
	l.Layout(tr.Root, 400, 300)
	n, _ := tr.ByID("page")
	if got := n.Base().Bounds().W; got != 120 {
		t.Fatalf("start width = %v, want 120", got)
	}

	n.Base().SetWidth(320)
	n.Base().SetHeight(180)
	l.Layout(tr.Root, 600, 300)
	if got := n.Base().Bounds(); got.W != 320 || got.H != 180 {
		t.Fatalf("after SetWidth = %v x %v, want 320 x 180", got.W, got.H)
	}

	n.Base().SetSize(200, 90)
	l.Layout(tr.Root, 600, 300)
	if got := n.Base().Bounds(); got.W != 200 || got.H != 90 {
		t.Fatalf("after SetSize = %v x %v, want 200 x 90", got.W, got.H)
	}
}
