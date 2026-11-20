package layout

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/tree"
)

// scrollPage is a 100dp-tall Column with three 60dp rows, so its maximum
// offset is 80.
func scrollPage(t *testing.T) (*tree.Tree, *tree.NodeBase) {
	t.Helper()
	tr := build(t, `Window {
		Column {
			id:      page
			height:  100
			spacing: 0
			Rectangle { id: a width: 10 height: 60 }
			Rectangle { id: b width: 10 height: 60 }
			Rectangle { id: c width: 10 height: 60 }
		}
	}`)
	New(nil).Layout(tr.Root, 400, 300)
	n, ok := tr.ByID("page")
	if !ok {
		t.Fatal("no page")
	}
	return tr, n.Base()
}

func TestScrollToHelpers(t *testing.T) {
	tr, page := scrollPage(t)

	page.ScrollTo(50)
	if got := page.ScrollOffset(); got != 50 {
		t.Fatalf("ScrollTo(50) = %v", got)
	}
	page.ScrollTo(-10)
	if got := page.ScrollOffset(); got != 0 {
		t.Fatalf("a negative offset reads %v", got)
	}
	page.ScrollTo(500)
	if got := page.ScrollOffset(); got != 80 {
		t.Fatalf("past the end reads %v, want 80", got)
	}

	page.ScrollTo(0)
	if !page.ScrollToNthChild(2) {
		t.Fatal("the second child was not found")
	}
	if got := page.ScrollOffset(); got != 60 {
		t.Fatalf("ScrollToNthChild(2) = %v, want 60", got)
	}
	if !page.ScrollToNthChild(2, tree.ScrollEnd) {
		t.Fatal("the second child was not found for end alignment")
	}
	if got := page.ScrollOffset(); got != 20 {
		t.Fatalf("end-aligned second child = %v, want 20", got)
	}
	if !page.ScrollToNthChild(2, tree.ScrollCenter) {
		t.Fatal("the second child was not found for center alignment")
	}
	if got := page.ScrollOffset(); got != 40 {
		t.Fatalf("centered second child = %v, want 40", got)
	}
	if !page.ScrollToChild("c", tree.ScrollEnd) {
		t.Fatal("the third child was not found by id")
	}
	if got := page.ScrollOffset(); got != 80 {
		t.Fatalf("end-aligned ScrollToChild(c) = %v, want 80", got)
	}
	if !page.ScrollToChild("c") {
		t.Fatal("the third child was not found by id")
	}
	if got := page.ScrollOffset(); got != 80 {
		t.Fatalf("ScrollToChild(c) = %v, want 80", got)
	}
	if page.ScrollToNthChild(4) {
		t.Fatal("a fourth child exists")
	}
	if page.ScrollToChild("nowhere") {
		t.Fatal("a missing id scrolled")
	}
	if tr.Err() != nil {
		t.Fatal(tr.Err())
	}
}

func TestScrollToSmoothEasesAndStops(t *testing.T) {
	tr, page := scrollPage(t)

	page.ScrollToSmooth(80, 100*time.Millisecond)
	if !tr.AdvanceTransitions(0.05) {
		t.Fatal("the smooth scroll did not ask for another frame")
	}
	if got := page.ScrollOffset(); got <= 0 || got >= 80 {
		t.Fatalf("halfway offset = %v, want between 0 and 80", got)
	}
	if !tr.AdvanceTransitions(0.06) {
		t.Fatal("the last frame did not report a change")
	}
	if got := page.ScrollOffset(); got != 80 {
		t.Fatalf("the smooth scroll ended at %v, want 80", got)
	}
	if tr.AdvanceTransitions(0.02) {
		t.Fatal("a finished smooth scroll kept the frame loop awake")
	}

	// The alignment also applies to a smooth child scroll.
	page.ScrollTo(0)
	page.ScrollToNthChildSmooth(2, 100*time.Millisecond, tree.ScrollCenter)
	tr.AdvanceTransitions(0.11)
	if got := page.ScrollOffset(); got != 40 {
		t.Fatalf("smooth centered second child = %v, want 40", got)
	}

	// A wheel or a drag writes the offset directly, so it cancels the flight.
	page.ScrollToSmooth(80, time.Second)
	page.SetScrollOffsetFor(tree.AxisY, 10)
	tr.AdvanceTransitions(0.5)
	if got := page.ScrollOffset(); got != 10 {
		t.Fatalf("a manual scroll left the animation running: %v", got)
	}
}

func TestScrollToNthChildOnARow(t *testing.T) {
	tr := build(t, `Window {
		Row {
			id:     strip
			width:  100
			height: 40
			Rectangle { width: 60 height: 10 }
			Rectangle { width: 60 height: 10 }
			Rectangle { width: 60 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 400, 300)
	n, _ := tr.ByID("strip")
	strip := n.Base()
	if !strip.ScrollToNthChild(2) {
		t.Fatal("the second card was not found")
	}
	if got := strip.ScrollOffset(); got != 60 {
		t.Fatalf("a horizontal row scrolled to %v, want 60", got)
	}
}

// TestScrollMethodsFromADesign drives the shortcuts a click can call by id,
// including the duration argument.
func TestScrollMethodsFromADesign(t *testing.T) {
	tr := build(t, `Window {
		Column {
			id:      page
			height:  100
			spacing: 0
			Button { id: jump label: "Jump" onClick: page.scrollTo(50) }
			Button { id: markButton label: "Mark" onClick: page.scrollToChild("mark", center) }
			Button { id: smoothButton label: "Smooth" onClick: page.scrollToSmooth(80, 500ms) }
			Column { id: mark width: 10 height: 60 }
			Rectangle { width: 10 height: 60 }
			Rectangle { width: 10 height: 60 }
		}
	}`)
	New(nil).Layout(tr.Root, 400, 300)
	page, _ := tr.ByID("page")
	mark, _ := tr.ByID("mark")

	jump, _ := tr.ByID("jump")
	jump.(*tree.Button).Base().Click.Emit(tree.ClickEvent{})
	if got := page.Base().ScrollOffset(); got != 50 {
		t.Fatalf("scrollTo(50) = %v", got)
	}
	markButton, _ := tr.ByID("markButton")
	markButton.(*tree.Button).Base().Click.Emit(tree.ClickEvent{})
	want := mark.Base().Bounds().Y + mark.Base().Bounds().H/2 - (page.Base().Bounds().Y + page.Base().Bounds().H/2)
	if got := page.Base().ScrollOffset(); got != want {
		t.Fatalf("scrollToChild(mark, center) = %v, want %v", got, want)
	}
	smoothButton, _ := tr.ByID("smoothButton")
	smoothButton.(*tree.Button).Base().Click.Emit(tree.ClickEvent{})
	if !tr.AdvanceTransitions(0.25) {
		t.Fatal("scrollToSmooth(80, 500ms) did not animate")
	}
	if tr.Err() != nil {
		t.Fatal(tr.Err())
	}
}
