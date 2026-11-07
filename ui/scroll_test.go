package ui

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/tree"
)

// TestTouchpadFlingCoasts checks that a touchpad gesture keeps scrolling for a
// moment and then stops on its own, so the loop can sleep. A wheel notch does
// not coast; that is TestWheelNotchDoesNotFling.
func TestTouchpadFlingCoasts(t *testing.T) {
	_, _, in := inputFixture(t, `Window { Column {
		scroll: true
		width: 100
		height: 60
		Rectangle { width: 100 height: 600 background: #ff0000 }
	} }`, 200, 200)
	in.cursorX, in.cursorY = 10, 10
	in.Scroll(0, -0.4, false) // a fraction of a notch: a touchpad, not a wheel
	n := in.flingNode
	if n == nil {
		t.Fatal("a touchpad step did not start a fling")
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

// TestScrollbarDragMovesThePage checks that grabbing the thumb and pulling it
// down scrolls the area, and that hideScrollbar removes the grab target.
func TestScrollbarDragMovesThePage(t *testing.T) {
	src := `Window { Column {
		id: page
		scroll: true
		autoHideScrollbar: false
		width: 100
		height: 100
		Rectangle { width: 100 height: 400 background: #ff0000 }
	} }`
	_, win, in := inputFixture(t, src, 100, 100)
	page := MustByID[*Column](win, "page")
	b := page.Base()
	inner := tree.Rect{X: 0, Y: 0, W: 100, H: 100}
	thumb, ok := b.ScrollbarThumb(inner)
	if !ok {
		t.Fatal("no thumb")
	}
	// Press the middle of the thumb and pull it to the bottom of the track.
	grabY := thumb.Y + thumb.H/2
	in.MouseDown(thumb.X+2, grabY, 0, false)
	if in.scrollbar == nil {
		t.Fatalf("press on the thumb did not grab it (thumb %+v)", thumb)
	}
	trackBottom := inner.Bottom() - thumb.H/2
	in.MouseMove(thumb.X+2, trackBottom)
	in.MouseUp(thumb.X+2, trackBottom, 0)
	if got, want := b.ScrollOffset(), b.ScrollMax(); got != want {
		t.Fatalf("offset = %v, want the end %v", got, want)
	}

	// A hidden bar is not a grab target.
	hidden := `Window { Column {
		id: page
		scroll: true
		autoHideScrollbar: false
		hideScrollbar: true
		width: 100
		height: 100
		Rectangle { width: 100 height: 400 background: #ff0000 }
	} }`
	_, win2, in2 := inputFixture(t, hidden, 100, 100)
	page2 := MustByID[*Column](win2, "page")
	if _, ok := page2.Base().ScrollbarThumb(inner); !ok {
		t.Fatal("geometry should still exist")
	}
	in2.MouseDown(96, 10, 0, false)
	if in2.scrollbar != nil {
		t.Fatal("a hidden bar still grabbed the press")
	}
}

// TestScrollbarAutohideFollowsPointer pins the default: the bar is down until
// the pointer enters the area or the area scrolls.
func TestScrollbarAutohideFollowsPointer(t *testing.T) {
	src := `Window { Column {
		id: page
		scroll: true
		width: 100
		height: 100
		Rectangle { width: 100 height: 400 background: #ff0000 }
	} }`
	_, win, in := inputFixture(t, src, 100, 100)
	page := MustByID[*Column](win, "page")
	b := page.Base()
	if b.ScrollbarVisible() {
		t.Fatal("bar visible before the pointer arrived")
	}
	in.MouseMove(50, 50)
	if !b.ScrollbarHover() || !b.ScrollbarVisible() {
		t.Fatal("bar not visible while the pointer is inside")
	}
	in.MouseLeave()
	if b.ScrollbarHover() {
		t.Fatal("hover survived the pointer leaving the window")
	}
	// A scroll re-shows it for the grace period even with the pointer outside.
	b.ShowScrollbar()
	if !b.ScrollbarVisible() {
		t.Fatal("a scroll did not show the bar")
	}
}

// TestOpacityPercent checks that a percentage is accepted for opacity and
// resolves against the 0..1 range during layout.
func TestOpacityPercent(t *testing.T) {
	_, win, _ := inputFixture(t, `Window {
		Column {
			id: page
			opacity: 50%
		}
	}`, 100, 100)
	if got := MustByID[*Column](win, "page").Base().Opacity(); got != 0.5 {
		t.Fatalf("opacity = %v, want 0.5", got)
	}
}

// TestScrollHitReachesTheEndEdge checks hit-testing uses the same clip as
// paint: the end padding is not a dead band, so a point in the bottom gutter
// still belongs to the overflowing content.
func TestScrollHitReachesTheEndEdge(t *testing.T) {
	_, _, in := inputFixture(t, `Window { Column {
		id: page
		scroll: true
		padding: 10
		width: 100
		height: 100
		Rectangle { id: body width: 80 height: 300 background: #ff0000 }
	} }`, 100, 100)
	if got := in.hit(50, 95); got == nil || got.Kind() != "Rectangle" {
		t.Fatalf("hit at the bottom edge = %v, want the scrolled body", got)
	}
	if got := in.hit(5, 50); got == nil || got.Kind() != "Column" {
		t.Fatalf("hit in the left padding = %v, want the page", got)
	}
}
