package layout_test

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestTextFieldSlotsSitInTheIconColumns: a field's $leading and $trailing
// bodies take the 40dp columns its icons occupy, so a reveal button or a clear
// mark lines up with the field without the design placing it.
func TestTextFieldSlotsSitInTheIconColumns(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			TextField {
				id:     field
				label:  "Password"
				width:  400
				height: 56

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
	n, _ := tr.ByID("field")
	field, ok := n.(*tree.TextField)
	if !ok || field.LeadingNode == nil || field.TrailingNode == nil {
		t.Fatal("the slots were not built")
	}
	lead := field.LeadingNode.Base().Bounds()
	if lead.X != 8 || lead.W != 40 || lead.H != 40 || lead.Y != 8 {
		t.Fatalf("leading box = %+v, want 8,8 40x40", lead)
	}
	trail := field.TrailingNode.Base().Bounds()
	if trail.X != 400-48 || trail.W != 40 || trail.H != 40 {
		t.Fatalf("trailing box = %+v", trail)
	}
}

// TestTextFieldIconWinsOverTheSlot: an explicit icon leaves the body it was
// given without bounds, so it neither paints nor takes a press.
func TestTextFieldIconWinsOverTheSlot(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			TextField {
				id:          field
				label:       "Email"
				width:       400
				leadingIcon: mail

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
	n, _ := tr.ByID("field")
	field := n.(*tree.TextField)
	if got := field.LeadingNode.Base().Bounds(); got.W != 0 || got.H != 0 {
		t.Fatalf("the slot kept bounds %+v while an icon is set", got)
	}
}

// TestTextFieldSupportingLineKeepsSlotsInTheBox: the helper line under the field
// is not part of the box the slots centre in, so a field with a supporting line
// lines its reveal button up with the value.
func TestTextFieldSupportingLineKeepsSlotsInTheBox(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			TextField {
				id:         field
				label:      "Password"
				supporting: "At least 8 characters"
				width:      400

				$trailing {
					Rectangle { width: 24 height: 24 }
				}
			}
		}
	} `), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("field")
	field := n.(*tree.TextField)
	if got := field.Base().Bounds(); got.H != 76 {
		t.Fatalf("field height = %v, want 76 with a supporting line", got.H)
	}
	trail := field.TrailingNode.Base().Bounds()
	if trail.Y != 8 || trail.H != 40 {
		t.Fatalf("trailing box = %+v, want it inside the 56dp field", trail)
	}
}
