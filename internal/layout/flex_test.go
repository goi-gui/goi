package layout_test

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestFlexRunsAlongItsDirection is the whole contract: the same children are a
// row at direction row and a column at direction column, spacing included.
func TestFlexRunsAlongItsDirection(t *testing.T) {
	build := func(dir string) (*tree.Tree, *tree.Flex) {
		t.Helper()
		tr, diag := tree.BuildSource([]byte(`Window { Column {
			Flex {
				id: f direction: `+dir+` spacing: 8
				Rectangle { id: a width: 20 height: 10 }
				Rectangle { id: b width: 20 height: 10 }
				Rectangle { id: c width: 20 height: 10 }
			}
		} }`), nil)
		if !diag.Empty() {
			t.Fatalf("%v", diag)
		}
		layout.New(nil).Layout(tr.Root, 400, 400)
		n, _ := tr.ByID("f")
		return tr, n.(*tree.Flex)
	}
	bounds := func(tr *tree.Tree, id string) tree.Rect {
		n, _ := tr.ByID(id)
		return n.Base().Bounds()
	}

	tr, flex := build("row")
	if got := bounds(tr, "b"); got.X != 28 || got.Y != 0 {
		t.Fatalf("row b = %+v, want 28,0", got)
	}
	if got := bounds(tr, "c"); got.X != 56 {
		t.Fatalf("row c.X = %v, want 56", got.X)
	}
	if got := flex.Base().Bounds(); got.H != 10 {
		t.Fatalf("row height = %v, want the tallest child", got.H)
	}

	tr, flex = build("column")
	if got := bounds(tr, "b"); got.X != 0 || got.Y != 18 {
		t.Fatalf("column b = %+v, want 0,18", got)
	}
	if got := bounds(tr, "c"); got.Y != 36 {
		t.Fatalf("column c.Y = %v, want 36", got.Y)
	}
	// Like a Column, a column-direction Flex spans its parent's width.
	if got := flex.Base().Bounds(); got.W != 400 || got.H != 46 {
		t.Fatalf("column box = %+v, want 400x46", got)
	}
}

// TestFlexDirectionFlipsWithoutRebuilding: direction is a property, so a Go call
// or an expression moves the children without touching the design.
func TestFlexDirectionFlipsWithoutRebuilding(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Flex {
			id: f direction: row spacing: 8
			Rectangle { id: a width: 20 height: 10 }
			Rectangle { id: b width: 20 height: 10 }
		}
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("f")
	f := n.(*tree.Flex)
	b, _ := tr.ByID("b")
	if got := b.Base().Bounds(); got.X != 28 || got.Y != 0 {
		t.Fatalf("row b = %+v", got)
	}

	f.Direction.Set("column")
	layout.New(nil).Layout(tr.Root, 400, 400)
	if got := b.Base().Bounds(); got.X != 0 || got.Y != 18 {
		t.Fatalf("column b = %+v, want 0,18 after the flip", got)
	}
}

// TestFlexSharesLeftoverSpace: a child's flex divides the row's spare width, the
// way it does in a Row, which is what makes the component a drop-in swap.
func TestFlexSharesLeftoverSpace(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Flex {
			direction: row spacing: 10 width: 310
			Rectangle { id: a flex: 1 height: 10 }
			Rectangle { id: b flex: 2 height: 10 }
		}
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	a, _ := tr.ByID("a")
	b, _ := tr.ByID("b")
	if a.Base().Bounds().W != 100 || b.Base().Bounds().W != 200 {
		t.Fatalf("flex widths = %v %v, want 100 and 200",
			a.Base().Bounds().W, b.Base().Bounds().W)
	}
}
