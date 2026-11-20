package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestPopoverChildUsesPlacedRect: a panel that shifts to stay inside a small
// window paints at the shifted spot, so a control inside it must hit there too.
// The colour picker's square and buttons are laid out from this rect.
func TestPopoverChildUsesPlacedRect(t *testing.T) {
	_, win := reloadApp(t, `Window {
		width: 300
		height: 200
		Popover {
			id: pop
			Button { label: "Open" }
			$popover {
				Column {
					id:     panel
					width:  220
					height: 180
					Text { text: "inside" }
				}
			}
		}
	}`)
	layout.New(layout.FakeMeasurer{}).Layout(win.tree.Root, 300, 200)
	win.tree.SetViewport(300, 200)

	pop, err := win.ByID[*tree.Popover]("pop")
	if err != nil {
		t.Fatal(err)
	}
	panel, err := win.ByID[*tree.Column]("panel")
	if err != nil {
		t.Fatal(err)
	}
	anchor := unshiftedAbsoluteRect(pop)
	unshifted := unshiftedAbsoluteRect(panel)
	w, h := win.tree.Viewport()
	dx, dy := pop.PanelShift(anchor, unshifted, w, h)
	if dx == 0 && dy == 0 {
		t.Fatal("the test panel did not need a shift")
	}
	got := absoluteRect(panel)
	if got.X != unshifted.X+dx || got.Y != unshifted.Y+dy {
		t.Fatalf("placed rect = %v, want shift %v,%v from %v", got, dx, dy, unshifted)
	}
}
