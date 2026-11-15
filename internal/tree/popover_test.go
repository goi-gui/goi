package tree

import "testing"

// TestPopoverPanelShift keeps the panel on screen: it flips above the anchor
// when below would run off the bottom, then slides in from the edges.
func TestPopoverPanelShift(t *testing.T) {
	p := &Popover{}
	anchor := Rect{X: 100, Y: 100, W: 80, H: 40}
	below := Rect{X: 100, Y: 148, W: 200, H: 96}
	if dx, dy := p.PanelShift(anchor, below, 800, 600); dx != 0 || dy != 0 {
		t.Fatalf("a panel that fits moved by %v,%v", dx, dy)
	}

	// Near the bottom: 548..644 does not fit in 600, so it flips above.
	anchor.Y = 500
	below = Rect{X: 100, Y: 548, W: 200, H: 96}
	dx, dy := p.PanelShift(anchor, below, 800, 600)
	if dx != 0 || dy != 500-PopoverGap-96-548 {
		t.Fatalf("flip moved by %v,%v, want %v,0", dx, dy, -152)
	}

	// Near the right edge: it slides in so its right edge meets the window.
	dx, dy = p.PanelShift(anchor, Rect{X: 700, Y: 148, W: 200, H: 96}, 800, 600)
	if dx != -100 || dy != 0 {
		t.Fatalf("right edge moved by %v,%v, want -100,0", dx, dy)
	}
	// Near the left edge: same, the other way.
	dx, _ = p.PanelShift(anchor, Rect{X: 6, Y: 148, W: 200, H: 96}, 800, 600)
	if dx != 0 {
		t.Fatalf("left edge moved by %v", dx)
	}
	dx, _ = p.PanelShift(anchor, Rect{X: -40, Y: 148, W: 200, H: 96}, 800, 600)
	if dx != 40 {
		t.Fatalf("off the left edge moved by %v, want 40", dx)
	}

	// A panel taller than the window stays at the top, not off screen.
	dx, dy = p.PanelShift(anchor, Rect{X: 0, Y: 0, W: 800, H: 900}, 800, 600)
	if dx != 0 || dy != 0 {
		t.Fatalf("an oversized panel moved by %v,%v", dx, dy)
	}
}

// TestPopoverPanelFloatsAboveLaterSiblings covers the paint order: the branch
// that holds a panel sorts after the siblings written after it, so a rect that
// covers the panel does not paint over it.
func TestPopoverPanelFloatsAboveLaterSiblings(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Popover {
				Button { label: "Open" }
				$popover { Card { label: "Panel" width: 120 height: 80 } }
			}
			Rectangle { width: 200 height: 120 }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	col := tr.Root.Base().Kids()[0]
	order := col.PaintOrder()
	if len(order) != 2 || order[1].Node().Kind() != "Popover" {
		t.Fatalf("paint order = %v, want the popover last", order)
	}
	// The panel itself sorts last among the popover's children, whatever the
	// design wrote first.
	panel := order[1].Node().(*Popover).Panel
	if panel == nil {
		t.Fatal("no panel")
	}
	kids := order[1].PaintOrder()
	if len(kids) == 0 || kids[len(kids)-1] != panel.Base() {
		t.Fatalf("the panel is not the last popover child: %v", kids)
	}
	if !panel.Base().floats || !order[1].floats {
		t.Fatal("the panel and its ancestors are not marked as floating")
	}
}
