package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/tree"
)

const snapPage = `Window { Column {
	id: page
	scroll: true
	scrollSnapType: %s
	scrollSnapStop: %s
	width: 200
	height: 300

	for range 4 {
		Rectangle {
			width:           200
			height:          200
			scrollSnapAlign: start
		}
	}
} }`

// snapFixture builds the scrolling page every test here uses, with the wheel
// pointer inside it.
func snapFixture(t *testing.T, kind, stop string) (*Window, *interactor, *tree.NodeBase) {
	t.Helper()
	src := sprintf(snapPage, kind, stop)
	_, win, in := inputFixture(t, src, 200, 300)
	in.cursorX, in.cursorY = 10, 10
	page := MustByID[*Column](win, "page").Base()
	return win, in, page
}

// sprintf fills the fixture template; two %s, in order.
func sprintf(format, a, b string) string {
	out := make([]byte, 0, len(format)+len(a)+len(b))
	args := []string{a, b}
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) && format[i+1] == 's' && len(args) > 0 {
			out = append(out, args[0]...)
			args = args[1:]
			i++
			continue
		}
		out = append(out, format[i])
	}
	return string(out)
}

// tickScroll runs the per-frame scroll work until nothing is left to animate.
func tickScroll(in *interactor) {
	for k := 0; k < 200; k++ {
		if !in.tick(0.016) {
			return
		}
	}
}

// TestWheelNotchSnapsOnePoint: a notched wheel on a mandatory container moves
// one snap point per notch, not a few pixels the settle would undo.
func TestWheelNotchSnapsOnePoint(t *testing.T) {
	_, in, page := snapFixture(t, "mandatory", "normal")
	in.Scroll(0, -1, false)
	tickScroll(in)
	if got := page.ScrollOffset(); got != 200 {
		t.Fatalf("offset after one notch = %v, want 200", got)
	}
	in.Scroll(0, -1, false)
	tickScroll(in)
	if got := page.ScrollOffset(); got != 400 {
		t.Fatalf("offset after two notches = %v, want 400", got)
	}
}

// TestDragReleaseSettlesOnASnapPoint: a hand that lets go between two blocks
// eases onto the nearer one instead of stopping in the gap.
func TestDragReleaseSettlesOnASnapPoint(t *testing.T) {
	_, in, page := snapFixture(t, "mandatory", "normal")
	in.MouseDown(10, 250, 0, false)
	in.MouseMove(10, 170) // 80dp down: between the first and second block
	in.MouseUp(10, 170, 0)
	tickScroll(in)
	if got := page.ScrollOffset(); got != 0 {
		t.Fatalf("settled at %v, want the nearer point 0", got)
	}
	// A pull past the midpoint goes to the next block.
	in.MouseDown(10, 250, 0, false)
	in.MouseMove(10, 40) // 210dp
	in.MouseUp(10, 40, 0)
	tickScroll(in)
	if got := page.ScrollOffset(); got != 200 {
		t.Fatalf("settled at %v, want 200", got)
	}
}

// TestProximityLeavesAFarReleaseAlone: proximity is a hint, so a reader who
// stops in the middle of a block keeps that place.
func TestProximityLeavesAFarReleaseAlone(t *testing.T) {
	_, in, page := snapFixture(t, "proximity", "normal")
	in.MouseDown(10, 250, 0, false)
	in.MouseMove(10, 160) // 90dp down: further than a quarter of the frame
	in.MouseUp(10, 160, 0)
	tickScroll(in)
	if got := page.ScrollOffset(); got != 90 {
		t.Fatalf("proximity moved a far stop to %v, want 90", got)
	}
}

// TestSnapStopStopsAFlingAtTheNextPoint: a fast flick crosses one block and
// settles there, instead of coasting through several.
func TestSnapStopStopsAFlingAtTheNextPoint(t *testing.T) {
	_, in, page := snapFixture(t, "mandatory", "stop")
	in.Scroll(0, -6, false) // a flick: a large, fractional touchpad delta
	tickScroll(in)
	if got := page.ScrollOffset(); got != 200 {
		t.Fatalf("a stop fling landed at %v, want the next point 200", got)
	}
}
