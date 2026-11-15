package ui

import "testing"

const outsideUI = `bridge {
	onClose func()
}

Window {
	Column {
		Popover {
			id:             pop
			onClickOutside: bridge.onClose()

			Button { id: anchor label: "Open" }

			$popover {
				Card { id: panel label: "Panel" width: 140 height: 80 }
			}
		}

		Text { id: elsewhere text: "elsewhere" }
	}
}`

// TestClickOutsideFiresOnAPressOutsideTheSubtree covers the event end to end:
// the anchor and the floating panel count as inside, another press reports its
// coordinates once, and a hidden element hears nothing.
func TestClickOutsideFiresOnAPressOutsideTheSubtree(t *testing.T) {
	_, win, in := inputFixture(t, outsideUI, 400, 300)
	fired := 0
	if err := SetHandler(win, "onClose", func() { fired++ }); err != nil {
		t.Fatal(err)
	}
	pop := MustByID[*Popover](win, "pop")
	var got []ClickEvent
	pop.OnClickOutside(func(e ClickEvent) { got = append(got, e) })

	press := func(r Rect) {
		in.MouseDown(r.X+r.W/2, r.Y+r.H/2, 0, false)
		in.MouseUp(r.X+r.W/2, r.Y+r.H/2, 0)
	}

	// The anchor and the panel are the element's own subtree.
	press(absoluteRect(MustByID[*Button](win, "anchor")))
	press(absoluteRect(MustByID[*Card](win, "panel")))
	if len(got) != 0 || fired != 0 {
		t.Fatalf("an inside press fired onClickOutside: %v, bridge %d", got, fired)
	}

	// Empty page space is outside it, and the press carries its coordinates.
	in.MouseDown(360, 250, 0, false)
	if len(got) != 1 || fired != 1 {
		t.Fatalf("%d outside events, bridge %d, want one of each", len(got), fired)
	}
	if got[0].X != 360 || got[0].Y != 250 || got[0].Button != 0 {
		t.Fatalf("event = %+v, want the press at 360,250", got[0])
	}
	in.MouseUp(360, 250, 0)
	if len(got) != 1 || fired != 1 {
		t.Fatalf("the release fired again: %d events, bridge %d", len(got), fired)
	}

	// A hidden popover is not on the page, so it hears nothing.
	pop.SetVisible(false)
	in.MouseDown(20, 250, 0, false)
	in.MouseUp(20, 250, 0)
	if len(got) != 1 || fired != 1 {
		t.Fatalf("a hidden popover fired: %d events, bridge %d", len(got), fired)
	}
}

// TestClickOutsideWaitsForThePanel keeps a mounted but closed popover quiet: the
// design still has the node, but nothing is showing, so there is nothing to
// dismiss.
func TestClickOutsideWaitsForThePanel(t *testing.T) {
	_, win, in := inputFixture(t, `bridge {
	open    bool
	onClose func()
}

Window {
	Column {
		Popover {
			id:             pop
			onClickOutside: bridge.onClose()

			Button { label: "Open" }

			if bridge.open {
				$popover { Card { label: "Panel" width: 140 height: 80 } }
			}
		}
	}
}`, 400, 300)
	fired := 0
	if err := SetHandler(win, "onClose", func() { fired++ }); err != nil {
		t.Fatal(err)
	}
	pop := MustByID[*Popover](win, "pop")
	if pop.Panel == nil || pop.Panel.Base().Visible() {
		t.Fatal("the panel should exist and be hidden")
	}
	in.MouseDown(360, 250, 0, false)
	in.MouseUp(360, 250, 0)
	if fired != 0 {
		t.Fatalf("a closed popover fired: %d", fired)
	}

	SetData(win, "open", true)
	win.tree.ApplyExpressions() // headless: the frame loop would do this
	in.MouseDown(360, 250, 0, false)
	in.MouseUp(360, 250, 0)
	if fired != 1 {
		t.Fatalf("an open popover fired %d times, want 1", fired)
	}
}
