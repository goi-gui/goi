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

// TestRemovingThePanelStopsTheLift keeps the float flags honest: a branch that
// held the only panel must not keep painting last after the panel is gone.
func TestRemovingThePanelStopsTheLift(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Column {
				id: holder
				Popover {
					id: pop
					Button { label: "Open" }
					$popover { Card { label: "Panel" } }
				}
			}
			Button { label: "After" }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	holder, _ := tr.ByID("holder")
	pop, _ := tr.ByID("pop")
	if !holder.Base().floats || !pop.Base().floats {
		t.Fatal("the panel did not lift its branch")
	}
	tr.Remove(pop)
	if holder.Base().floats {
		t.Fatal("the holder still floats after its panel was removed")
	}
	order := holder.Base().PaintOrder()
	if len(order) != 0 {
		t.Fatalf("holder children = %d, want none", len(order))
	}
}

// TestZIndexBeatsAFloatingBranch is the drawer-behind-a-page bug: a page that
// holds a popover lifts its branch above later siblings, and a modal written
// after it lost the order. An explicit zIndex is the stronger claim, so the
// overlay wins; at equal zIndex the floating branch still wins, which is what
// keeps a panel above the siblings written after its host.
func TestZIndexBeatsAFloatingBranch(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window { Stack {
		Column {
			Popover {
				Button { label: "Open" }
				$popover { Card { label: "Panel" width: 80 height: 40 } }
			}
		}
		Rectangle { id: scrim zIndex: 1 }
		Positioned { id: drawer zIndex: 2 left: 0 top: 0 bottom: 0 width: 200 }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	stack := tr.Root.Children()[0].Base()
	order := stack.PaintOrder()
	last := order[len(order)-1]
	if last.ID() != "drawer" {
		t.Fatalf("the drawer is not painted last: %v", order)
	}

	// Without the zIndex, the same stack orders the floating branch last.
	tr, diag = BuildSource([]byte(`Window { Stack {
		Column {
			Popover {
				Button { label: "Open" }
				$popover { Card { label: "Panel" width: 80 height: 40 } }
			}
		}
		Rectangle { id: later }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	stack = tr.Root.Children()[0].Base()
	order = stack.PaintOrder()
	if last := order[len(order)-1]; last.Node().Kind() != "Column" {
		t.Fatalf("the floating branch is not painted last: %v", order)
	}
}

// TestPopoverShowAndToggle covers the panel state from Go and from a design
// event: the panel's own visibility is the one source, so layout, paint and
// hit-testing follow.
func TestPopoverShowAndToggle(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Popover {
			id: pop
			Button { id: flipper label: "Flip" onClick: pop.toggle() }
			Button { id: hider label: "Hide" onClick: pop.show(false) }
			$popover { Card { label: "Panel" width: 100 height: 60 } }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, _ := tr.ByID("pop")
	p := n.(*Popover)
	if p.Panel == nil || !p.Panel.Base().Visible() {
		t.Fatal("the panel did not start visible")
	}
	p.Show(false)
	if p.Panel.Base().Visible() {
		t.Fatal("Show(false) left the panel visible")
	}
	p.Show(true)
	if !p.Panel.Base().Visible() {
		t.Fatal("Show(true) did not show the panel")
	}
	if p.Toggle() || p.Panel.Base().Visible() {
		t.Fatal("Toggle did not hide the panel")
	}
	if !p.Toggle() || !p.Panel.Base().Visible() {
		t.Fatal("Toggle did not show the panel again")
	}

	// The same two methods from a design event.
	btn, _ := tr.ByID("flipper")
	btn.(*Button).Click.Emit(ClickEvent{})
	if p.Panel.Base().Visible() {
		t.Fatal("pop.toggle() did not hide the panel")
	}
	btn.(*Button).Click.Emit(ClickEvent{})
	if !p.Panel.Base().Visible() {
		t.Fatal("pop.toggle() did not show the panel")
	}
	hider, _ := tr.ByID("hider")
	hider.(*Button).Click.Emit(ClickEvent{})
	if p.Panel.Base().Visible() {
		t.Fatal("pop.show(false) did not hide the panel")
	}
}
