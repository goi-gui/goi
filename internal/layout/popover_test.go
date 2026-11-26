package layout

import (
	"testing"

	"github.com/goi-gui/goi/internal/tree"
)

// TestPopoverPanelHangsUnderTheAnchor covers the whole point of the component:
// the anchor lays out as a row, the panel floats under it with the gap, and
// `align` lines the panel up with the anchor's own box.
func TestPopoverPanelHangsUnderTheAnchor(t *testing.T) {
	for _, tc := range []struct {
		align string
		x     func(anchor, panel tree.Rect) float64
	}{
		{"start", func(a, _ tree.Rect) float64 { return a.X }},
		{"center", func(a, p tree.Rect) float64 { return a.X + (a.W-p.W)/2 }},
		{"end", func(a, p tree.Rect) float64 { return a.Right() - p.W }},
	} {
		t.Run(tc.align, func(t *testing.T) {
			tr, diag := tree.BuildSource([]byte(`Window {
				Column {
					Popover {
						id: pop
						align: `+tc.align+`
						show: true
						Button { id: first label: "Open" }
						IconButton { id: second icon: add }
						$popover {
							Card { id: card width: 200 height: 100 }
						}
					}
				}
			}`), nil)
			if !diag.Empty() {
				t.Fatal(diag)
			}
			New(nil).Layout(tr.Root, 640, 480)
			first, _ := tr.ByID("first")
			second, _ := tr.ByID("second")
			card, _ := tr.ByID("card")
			pop, _ := tr.ByID("pop")

			anchor := first.Base().Bounds()
			// A row, so the two anchors sit side by side.
			if got := second.Base().Bounds().X; got <= anchor.X {
				t.Fatalf("second anchor x = %v, want it after %v", got, anchor.X)
			}
			anchor.W = second.Base().Bounds().Right() - anchor.X

			panel := card.Base().Parent().Base().Bounds()
			if got, want := panel.Y, anchor.Bottom()+tree.PopoverGap; got != want {
				t.Errorf("panel y = %v, want %v", got, want)
			}
			if got, want := panel.X, tc.x(anchor, panel); got != want {
				t.Errorf("panel x = %v, want %v", got, want)
			}
			// The popover's own box is the anchor, not the floating panel.
			if got := pop.Base().Bounds(); got.H != anchor.H || got.W != anchor.W {
				t.Errorf("popover box = %+v, want the anchor's %+v", got, anchor)
			}
		})
	}
}
