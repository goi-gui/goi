package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestPopoverPanelPaintsAboveAndFlips covers the two overlay rules at once: the
// panel paints after a later sibling that overlaps it, and it flips above the
// anchor when it would run off the bottom of the window.
func TestPopoverPanelPaintsAboveAndFlips(t *testing.T) {
	src := `Window {
		Column {
			height:        100%
			mainAxisAlign: end

			Rectangle { width: 200 height: 120 background: #00ff00 }

			Popover {
				Button { label: "Open" }
				$popover {
					Card { id: panel label: "Panel" width: 120 height: 80 }
				}
			}
		}
	}`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 200)
	l := &List{}
	l.Build(tr, 400, 200)

	cover, panel := -1, -1
	for i, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		if op.Color == 0x00ff00ff && cover < 0 {
			cover = i
			continue
		}
		if op.Rect[2] == 120 && op.Rect[3] == 80 && panel < 0 {
			panel = i
		}
	}
	if cover < 0 {
		t.Fatal("the covering rect did not paint")
	}
	if panel < 0 {
		t.Fatal("the panel did not paint")
	}
	if panel < cover {
		t.Fatal("the panel painted before the sibling that covers it")
	}
	// The anchor sits on the bottom edge (160..200), so a panel below would
	// leave the 200dp window and flips to 72.
	if got := l.Ops[panel].Rect[1]; got != 72 {
		t.Fatalf("panel y = %v, want it flipped above the anchor", got)
	}
}

// TestPopoverPanelClipsToItsScroller is the panel-inside-a-long-page bug: an
// anchor below the visible part of a scrolling column must not paint its panel
// over the page, and scrolling down to the anchor brings the panel with it. The
// scroller sits inside another column, so its viewport is not the window: a
// panel clipped to the window would still slip past the frame above it.
func TestPopoverPanelClipsToItsScroller(t *testing.T) {
	src := `Window {
		Column {
			Rectangle { height: 40 background: #0000ff }

			Column {
				id:     page
				scroll: true
				width:  300
				height: 150

				Rectangle { width: 300 height: 400 background: #00ff00 }

				Popover {
					id: pop
					Button { label: "Open" }
					$popover {
						Card { label: "Panel" width: 120 height: 80 }
					}
				}
			}
		}
	}`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 300, 220)
	page, _ := tr.ByID("page")
	// The panel is the 120x80 card, wherever scrolling put it.
	panelOps := func(l *List) (ops []Op) {
		for _, op := range l.Ops {
			if op.Rect[2] == 120 && op.Rect[3] == 80 {
				ops = append(ops, op)
			}
		}
		return ops
	}

	// Before scrolling, the panel is below the frame at y 40..190. Its clip must
	// be that frame, not the window, so nothing of it reaches the screen.
	l := &List{}
	l.Build(tr, 300, 220)
	ops := panelOps(l)
	if len(ops) == 0 {
		t.Fatal("the panel painted nothing, so the test proves nothing")
	}
	for _, op := range ops {
		if op.Clip[1] < 40 || op.Clip[1]+op.Clip[3] > 190.5 {
			t.Fatalf("panel op at %v is clipped to %v, want the scroller frame 40..190", op.Rect, op.Clip)
		}
	}

	// Scroll to the end: the anchor is in view, and the panel draws inside the
	// same frame instead of over the page.
	page.Base().SetScrollOffset(page.Base().ScrollMaxFor(tree.AxisY))
	layout.New(nil).Layout(tr.Root, 300, 220)
	l = &List{}
	l.Build(tr, 300, 220)
	inside := 0
	for _, op := range panelOps(l) {
		if op.Clip[1] < 40 || op.Clip[1]+op.Clip[3] > 190.5 {
			t.Fatalf("after scrolling, a panel op is clipped to %v", op.Clip)
		}
		if op.Rect[1] < 190 && op.Rect[1]+op.Rect[3] > 40 {
			inside++
		}
	}
	if inside == 0 {
		t.Fatal("the panel did not paint once its anchor scrolled into view")
	}
}
