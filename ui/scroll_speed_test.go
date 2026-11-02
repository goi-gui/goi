package ui

import (
	"fmt"
	"math"
	"testing"

	"github.com/ksckaan1/goi/internal/tree"
)

// scrollSpeedUI is deliberately long, so a fling is measured rather than
// clamped at the end of the content.
const scrollSpeedUI = `Window {
	Column {
		id: page
		scroll: true
		width: 300
		height: 300
		Column { height: 6000 width: 300 }
	}
}`

// scrollFixture returns an interactor whose pointer sits inside a long page.
func scrollFixture(t *testing.T) (*interactor, tree.Node) {
	t.Helper()
	_, win, in := inputFixture(t, scrollSpeedUI, 300, 300)
	in.cursorX, in.cursorY = 50, 50
	in.MouseMove(50, 50)
	n, ok := win.tree.ByID("page")
	if !ok {
		t.Fatal("no scrollable page")
	}
	return in, n
}

// One notch of a wheel is one step: it must not coast afterwards, or a single
// click of the wheel travels several times as far as the notch.
func TestWheelNotchDoesNotFling(t *testing.T) {
	in, n := scrollFixture(t)
	in.Scroll(0, -1) // one notch down
	if got := n.Base().ScrollOffset(); got != scrollStep {
		t.Fatalf("offset = %v, want one step %v", got, float64(scrollStep))
	}
	if in.flingNode != nil && in.flingV != 0 {
		t.Fatalf("a wheel notch started a fling of %v px/s", in.flingV)
	}
	// And nothing moves afterwards.
	before := n.Base().ScrollOffset()
	for i := 0; i < 30; i++ {
		in.tickFling(1.0 / 60)
	}
	if got := n.Base().ScrollOffset(); got != before {
		t.Fatalf("the page kept moving after the notch: %v", got)
	}
}

// A spin of the wheel must add up to one step per notch, no more.
func TestWheelSpinStaysLinear(t *testing.T) {
	in, n := scrollFixture(t)
	for i := 0; i < 5; i++ {
		in.Scroll(0, -1)
	}
	if got, want := n.Base().ScrollOffset(), 5*float64(scrollStep); got != want {
		t.Fatalf("five notches moved %v, want %v", got, want)
	}
}

// A touchpad flick still coasts, but the coast is bounded: a hard flick must not
// throw the content past what the reader aimed at.
func TestTouchpadFlingIsCapped(t *testing.T) {
	in, n := scrollFixture(t)
	// 40 precise events in 0.4s, as a fast two-finger swipe delivers.
	for i := 0; i < 40; i++ {
		in.Scroll(0, -0.5)
	}
	gesture := n.Base().ScrollOffset()

	coast := 0.0
	for i := 0; i < 120; i++ {
		before := n.Base().ScrollOffset()
		in.tickFling(1.0 / 60)
		coast += n.Base().ScrollOffset() - before
	}
	if coast == 0 {
		t.Fatal("a touchpad flick did not coast at all")
	}
	if max := float64(flingMaxSpeed) / flingFriction; coast > max*1.05 {
		t.Fatalf("coasted %v px, more than the %v cap", coast, max)
	}
	if coast > gesture {
		t.Fatalf("coasted %v px after a %v px gesture; the fling dominates", coast, gesture)
	}
}

// A slow, deliberate touchpad drag must not turn into a fling, which is what
// makes precise positioning feel impossible.
func TestTouchpadSlowScrollBarelyCoasts(t *testing.T) {
	in, n := scrollFixture(t)
	for i := 0; i < 10; i++ {
		in.Scroll(0, -0.2)
		sleepMS(20)
	}
	gesture := n.Base().ScrollOffset()
	coast := 0.0
	for i := 0; i < 120; i++ {
		before := n.Base().ScrollOffset()
		in.tickFling(1.0 / 60)
		coast += n.Base().ScrollOffset() - before
	}
	if math.Abs(coast) > math.Abs(gesture) {
		t.Fatalf("a slow drag of %v px coasted %v px", gesture, coast)
	}
}

// nestedScrollUI puts a scrollable column inside a scrollable page, so a wheel
// over the inner one has two candidates.
const nestedScrollUI = `Window {
	Column {
		id: page
		scroll: true
		width: 300
		height: 300
		Column {
			id: inner
			scroll: true
			width: 300
			height: 100
			Column { height: %d width: 300 }
		}
		Column { height: 600 width: 300 }
	}
}`

// nestedScrollFixture returns the interactor with the pointer over the inner
// scroller, plus both nodes.
func nestedScrollFixture(t *testing.T, innerContent int) (*interactor, tree.Node, tree.Node) {
	t.Helper()
	_, win, in := inputFixture(t, fmt.Sprintf(nestedScrollUI, innerContent), 300, 300)
	in.MouseMove(50, 50)
	page, ok := win.tree.ByID("page")
	if !ok {
		t.Fatal("no page")
	}
	inner, ok := win.tree.ByID("inner")
	if !ok {
		t.Fatal("no inner scroller")
	}
	return in, inner, page
}

// The inner scroller takes the wheels it can use, and the page gets the rest
// once it is at its end: the pointer must never look stuck over a child.
func TestWheelChainsFromInnerToPage(t *testing.T) {
	in, inner, page := nestedScrollFixture(t, 200) // 100px of room
	in.Scroll(0, -1)
	if got := inner.Base().ScrollOffset(); got != 48 {
		t.Fatalf("inner = %v after one notch, want 48", got)
	}
	if got := page.Base().ScrollOffset(); got != 0 {
		t.Fatalf("page moved to %v while the inner had room", got)
	}
	in.Scroll(0, -1)
	if got := inner.Base().ScrollOffset(); got != 96 {
		t.Fatalf("inner = %v after two notches, want 96", got)
	}
	in.Scroll(0, -1) // 4px left inside, 44px for the page
	if got := inner.Base().ScrollOffset(); got != 100 {
		t.Fatalf("inner = %v, want its end at 100", got)
	}
	if got := page.Base().ScrollOffset(); got != 44 {
		t.Fatalf("page = %v, want the leftover 44", got)
	}
}

// A scrollable child that cannot scroll at all must not block the page: this
// is what a short TextArea inside a long page looks like.
func TestWheelPassesThroughAnInnerThatCannotScroll(t *testing.T) {
	in, inner, page := nestedScrollFixture(t, 40)
	in.Scroll(0, -1)
	if got := inner.Base().ScrollOffset(); got != 0 {
		t.Fatalf("a non-overflowing child scrolled to %v", got)
	}
	if got := page.Base().ScrollOffset(); got != 48 {
		t.Fatalf("page = %v, want one notch 48", got)
	}
}

// A coast that reaches the end of the inner scroller carries on into the page.
func TestTouchpadFlingChainsToThePage(t *testing.T) {
	in, inner, page := nestedScrollFixture(t, 200)
	inner.Base().SetScrollOffset(100) // the inner is already at its end
	in.flingNode, in.flingV = inner, 1000
	if !in.tickFling(1.0 / 60) {
		t.Fatal("the coast stopped at the inner end instead of moving the page")
	}
	if got := page.Base().ScrollOffset(); got == 0 {
		t.Fatal("page did not move")
	}
}
