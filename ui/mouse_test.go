package ui

import "testing"

// TestGestureDetectorReportsMouseEvents covers the three pointer events: enter
// once, a move per pointer move, and leave when the pointer goes elsewhere or
// leaves the window, with window coordinates in the Go payload.
func TestGestureDetectorReportsMouseEvents(t *testing.T) {
	_, win, in := inputFixture(t, `bridge {
	onEnter func()
	onMove  func()
	onLeave func()
}

Window {
	Column {
		GestureDetector {
			id:             gd
			width:          120
			height:         80
			onMouseEnter:   bridge.onEnter()
			onMouseMove:    bridge.onMove()
			onMouseLeave:   bridge.onLeave()

			Rectangle { background: #ff0000 }
		}
	}
}`, 300, 300)
	enter, move, leave := 0, 0, 0
	for name, counter := range map[string]*int{
		"onEnter": &enter, "onMove": &move, "onLeave": &leave,
	} {
		n := counter
		if err := SetHandler(win, name, func() { *n++ }); err != nil {
			t.Fatal(err)
		}
	}
	gd := MustByID[*GestureDetector](win, "gd")
	var last MouseEvent
	gd.OnMouseMove(func(e MouseEvent) { last = e })

	in.MouseMove(10, 10)
	if enter != 1 || move != 1 || leave != 0 {
		t.Fatalf("enter=%d move=%d leave=%d after entering", enter, move, leave)
	}
	if last.X != 10 || last.Y != 10 {
		t.Fatalf("move payload = %+v, want 10,10", last)
	}

	in.MouseMove(60, 40)
	if enter != 1 || move != 2 || leave != 0 {
		t.Fatalf("enter=%d move=%d leave=%d after a move inside", enter, move, leave)
	}

	in.MouseMove(250, 250)
	if enter != 1 || move != 2 || leave != 1 {
		t.Fatalf("enter=%d move=%d leave=%d after leaving", enter, move, leave)
	}

	// Leaving the window reports the last position, not a move first.
	in.MouseMove(10, 10)
	in.MouseLeave()
	if leave != 2 {
		t.Fatalf("leave=%d after the pointer left the window, want 2", leave)
	}
	if last.X != 10 || last.Y != 10 {
		t.Fatalf("unexpected payload %+v", last)
	}
}
