package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestPopoverPanelPaintsAboveAndFlips covers the two overlay rules at once: the
// panel paints after a later sibling that overlaps it, and it flips above the
// anchor when it would run off the bottom of the window.
func TestPopoverPanelPaintsAboveAndFlips(t *testing.T) {
	src := `Window {
		Column {
			height:        100%
			mainAxisAlign: end

			Rectangle { width: 200 height: 120 background: #00ff00 }

			Popover {
				Button { label: "Open" }
				$popover {
					Card { id: panel label: "Panel" width: 120 height: 80 }
				}
			}
		}
	}`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 200)
	l := &List{}
	l.Build(tr, 400, 200)

	cover, panel := -1, -1
	for i, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		if op.Color == 0x00ff00ff && cover < 0 {
			cover = i
			continue
		}
		if op.Rect[2] == 120 && op.Rect[3] == 80 && panel < 0 {
			panel = i
		}
	}
	if cover < 0 {
		t.Fatal("the covering rect did not paint")
	}
	if panel < 0 {
		t.Fatal("the panel did not paint")
	}
	if panel < cover {
		t.Fatal("the panel painted before the sibling that covers it")
	}
	// The anchor sits on the bottom edge (160..200), so a panel below would
	// leave the 200dp window and flips to 72.
	if got := l.Ops[panel].Rect[1]; got != 72 {
		t.Fatalf("panel y = %v, want it flipped above the anchor", got)
	}
}
