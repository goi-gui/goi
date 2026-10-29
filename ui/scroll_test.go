package ui

import (
	"testing"
	"time"
)

// TestWheelFlingCoasts checks that a wheel gesture keeps scrolling for a moment
// and then stops on its own, so the loop can sleep.
func TestWheelFlingCoasts(t *testing.T) {
	_, _, in := inputFixture(t, `Window { Column {
		scroll: true
		width: 100
		height: 60
		Rectangle { width: 100 height: 600 background: #ff0000 }
	} }`, 200, 200)
	in.cursorX, in.cursorY = 10, 10
	in.Scroll(0, -1)
	n := in.flingNode
	if n == nil {
		t.Fatal("a wheel step did not start a fling")
	}
	time.Sleep(10 * time.Millisecond)
	before := n.Base().ScrollOffset()
	if !in.tickFling(0.016) {
		t.Fatal("the fling did not ask for a frame")
	}
	if n.Base().ScrollOffset() <= before {
		t.Fatalf("offset did not advance: %v", n.Base().ScrollOffset())
	}
	for k := 0; k < 200 && in.tickFling(0.016); k++ {
	}
	if in.flingNode != nil {
		t.Fatal("the fling never stopped")
	}
}
