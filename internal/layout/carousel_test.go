package layout

import (
	"testing"

	"github.com/goi-gui/goi/internal/tree"
)

// TestCarouselStretchesAndSnaps is the carousel's contract: items take the
// strip's height, a carousel item without a width gets the card default, and
// every item is a snap point at its leading edge.
func TestCarouselStretchesAndSnaps(t *testing.T) {
	tr := build(t, `Window {
		Carousel {
			id:     strip
			width:  300
			height: 200
			gap:    10

			CarouselItem {
				id:    first
				width: 180
				Rectangle { id: firstFill }
			}

			CarouselItem {
				id:    second
				Rectangle { id: secondFill }
			}

			CarouselItem {
				id:    third
				width: 180
				Rectangle { }
			}
		}
	}`)
	New(nil).Layout(tr.Root, 600, 400)

	strip, _ := tr.ByID("strip")
	if got := strip.Base().Radius(); got != 0 {
		t.Fatalf("carousel radius = %v", got)
	}
	first, _ := tr.ByID("first")
	second, _ := tr.ByID("second")
	third, _ := tr.ByID("third")
	want(t, first.Base().Bounds(), 0, 0, 180, 200)
	want(t, second.Base().Bounds(), 190, 0, 180, 200)
	want(t, third.Base().Bounds(), 380, 0, 180, 200)
	if first.Base().Radius() != 28 || second.Base().Radius() != 28 {
		t.Fatalf("item radius = %v/%v, want 28", first.Base().Radius(), second.Base().Radius())
	}
	if got := first.Base().SnapAlign(); got != "start" {
		t.Fatalf("item snap align = %q", got)
	}
	want(t, bounds(t, tr, "firstFill"), 0, 0, 180, 200)
	want(t, bounds(t, tr, "secondFill"), 0, 0, 180, 200)

	targets := strip.Base().SnapTargets(tree.AxisX)
	if len(targets) != 3 || targets[0] != 0 || targets[1] != 190 || targets[2] != 260 {
		t.Fatalf("snap targets = %v, want [0 190 260]", targets)
	}
}
