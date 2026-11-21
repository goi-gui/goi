package layout

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/tree"
)

// TestScrollEventsReportPositionAndEdges covers the three handlers a chat or a
// feed uses: every move, reaching the end and returning to the start.
func TestScrollEventsReportPositionAndEdges(t *testing.T) {
	tr := build(t, `bridge { items []string }
Window {
	Column {
		id:     page
		scroll: true
		height: 100
		for _, item := range bridge.items {
			Text { text: item height: 40 }
		}
	}
}`)
	tr.SetData("items", []string{"a", "b", "c", "d", "e"})
	e := New(nil)
	e.Layout(tr.Root, 300, 200)
	page, _ := tr.ByID("page")
	b := page.Base()

	var moves []tree.ScrollEvent
	b.OnScroll(func(ev tree.ScrollEvent) { moves = append(moves, ev) })
	var end, start int
	b.OnScrollToEnd(func() { end++ })
	b.OnScrollToStart(func() { start++ })

	b.SetScrollOffsetForBy(tree.AxisY, 50, true)
	max := b.ScrollMaxFor(tree.AxisY)
	if max <= 0 {
		t.Fatal("the page did not overflow")
	}
	if len(moves) != 1 || moves[0].Y != 50 || moves[0].MaxY != max ||
		moves[0].Progress != 50/max || !moves[0].ByUser {
		t.Fatalf("moves = %+v, max = %v", moves, max)
	}
	b.SetScrollOffsetForBy(tree.AxisY, max, true)
	if end != 1 || start != 0 {
		t.Fatalf("after end: end=%d start=%d", end, start)
	}
	b.SetScrollOffsetForBy(tree.AxisY, 0, true)
	if start != 1 || moves[len(moves)-1].Progress != 0 {
		t.Fatalf("back at start: start=%d progress=%v", start, moves[len(moves)-1].Progress)
	}
	// Repeating the same offset is not a move and does not repeat the edge.
	b.SetScrollOffsetForBy(tree.AxisY, 0, true)
	if len(moves) != 3 || start != 1 {
		t.Fatalf("repeat: moves=%d start=%d", len(moves), start)
	}
}

// TestAutoScrollFollowsChat is the chat rule: follow content growth while the
// reader is at the end, pause after a user scroll up, and resume at the bottom.
func TestAutoScrollFollowsChat(t *testing.T) {
	tr := build(t, `bridge { items []string }
Window {
	Column {
		id:     page
		scroll: true
		height: 100
		for _, item := range bridge.items {
			Text { text: item height: 40 }
		}
	}
}`)
	e := New(nil)
	tr.SetData("items", []string{"1", "2", "3"})
	e.Layout(tr.Root, 300, 200)
	page, _ := tr.ByID("page")
	b := page.Base()
	b.AutoScrollSmooth(true, 100*time.Millisecond)
	e.Layout(tr.Root, 300, 200)
	tr.ApplyAutoScroll()
	tr.AdvanceTransitions(0.2)
	if got, max := b.ScrollOffset(), b.ScrollMax(); got != max {
		t.Fatalf("initial follow = %v, want end %v", got, max)
	}

	// A new row keeps the end in view.
	tr.SetData("items", []string{"1", "2", "3", "4", "5"})
	e.Layout(tr.Root, 300, 200)
	tr.ApplyAutoScroll()
	tr.AdvanceTransitions(0.2)
	if got, max := b.ScrollOffset(), b.ScrollMax(); got != max {
		t.Fatalf("grown follow = %v, want end %v", got, max)
	}

	// Reading history pauses the follow.
	b.SetScrollOffsetForBy(tree.AxisY, 0, true)
	tr.SetData("items", []string{"1", "2", "3", "4", "5", "6"})
	e.Layout(tr.Root, 300, 200)
	tr.ApplyAutoScroll()
	if got := b.ScrollOffset(); got != 0 {
		t.Fatalf("paused page moved to %v", got)
	}

	// Returning to the bottom resumes it.
	b.SetScrollOffsetForBy(tree.AxisY, b.ScrollMax(), true)
	tr.SetData("items", []string{"1", "2", "3", "4", "5", "6", "7"})
	e.Layout(tr.Root, 300, 200)
	tr.ApplyAutoScroll()
	tr.AdvanceTransitions(0.2)
	if got, max := b.ScrollOffset(), b.ScrollMax(); got != max {
		t.Fatalf("resumed follow = %v, want end %v", got, max)
	}
}
