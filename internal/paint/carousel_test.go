package paint

import "testing"

// TestCarouselItemPaintsOverlay: the item's own surface is behind its child,
// and the label and scrim are painted after the child and clipped to the card's
// rounded corners.
func TestCarouselItemPaintsOverlay(t *testing.T) {
	l := list(t, `Window {
		Carousel {
			height: 200
			CarouselItem {
				width: 180
				label: "One"
				Rectangle { background: #ff0000 }
			}
		}
	}`, 600, 400)

	var label, scrim, child bool
	for _, op := range l.Ops {
		switch {
		case op.Kind == Text && op.Text == "One":
			label = true
			if op.Color != 0xFFFFFFFF {
				t.Fatalf("label colour = %08x", op.Color)
			}
		case op.Kind == Fill && op.Shape == CSSGradient && op.Gradient != nil:
			scrim = true
			stops := op.Gradient.Stops
			if len(stops) != 2 || stops[0].Color != 0x00000000 || stops[1].Color != 0x00000099 {
				t.Fatalf("scrim = %+v", stops)
			}
		case op.Kind == Fill && op.Color == 0xFF0000FF:
			child = true
			if op.ClipRadius != 28 {
				t.Fatalf("child clip radius = %v, want 28", op.ClipRadius)
			}
		}
	}
	if !label || !scrim || !child {
		t.Fatalf("label=%v scrim=%v child=%v", label, scrim, child)
	}
}
