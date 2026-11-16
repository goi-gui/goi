package layout_test

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestListItemSlotsSitInTheIconSquares: the bodies a row is given are centred in
// the 40dp squares the icons occupy, so a leading avatar and a trailing switch
// line up with the text without the design placing them.
func TestListItemSlotsSitInTheIconSquares(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			ListItem {
				id:         row
				label:      "Row"
				supporting: "Two lines"

				$leading {
					Rectangle { width: 24 height: 24 }
				}

				$trailing {
					Rectangle { width: 16 height: 16 }
				}
			}
		}
	} `), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("row")
	row, ok := n.(*tree.ListItem)
	if !ok || row.LeadingNode == nil || row.TrailingNode == nil {
		t.Fatal("the slots were not built")
	}
	if got := row.Base().Bounds(); got.H != 72 {
		t.Fatalf("row height = %v, want 72 for two lines", got.H)
	}
	lead := row.LeadingNode.Base().Bounds()
	if lead.X != 16 || lead.W != 40 || lead.H != 40 || lead.Y != 16 {
		t.Fatalf("leading box = %+v, want 16,16 40x40", lead)
	}
	trail := row.TrailingNode.Base().Bounds()
	if trail.X != 400-16-40 || trail.W != 40 || trail.H != 40 {
		t.Fatalf("trailing box = %+v", trail)
	}
}

// TestListItemIconWinsOverTheSlot: an explicit icon leaves the body it was given
// without bounds, so it neither paints nor takes a press.
func TestListItemIconWinsOverTheSlot(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			ListItem {
				id:          row
				label:       "Row"
				leadingIcon: home

				$leading {
					Rectangle { width: 24 height: 24 }
				}
			}
		}
	} `), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("row")
	row := n.(*tree.ListItem)
	if got := row.LeadingNode.Base().Bounds(); got.W != 0 || got.H != 0 {
		t.Fatalf("the slot kept bounds %+v while an icon is set", got)
	}
}
