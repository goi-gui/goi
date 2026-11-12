package paint

import "testing"

// TestBackdropBlurSitsUnderTheFill checks the order the renderer relies on: the
// backdrop op is the first thing the element paints, so the blurred copy of the
// scene lands under its own translucent background.
func TestBackdropBlurSitsUnderTheFill(t *testing.T) {
	l := list(t, `Window {
		Column {
			background: #ff0000
			Rectangle {
				width: 200
				height: 100
				radius: 12
				background: #00000044
				backdropBlur: 6
			}
		}
	}`, 400, 300)

	backdrop, fill := -1, -1
	for i, op := range l.Ops {
		if op.Kind == Backdrop {
			if backdrop >= 0 {
				t.Fatalf("more than one backdrop op: %d and %d", backdrop, i)
			}
			backdrop = i
			if op.Blur != 6 {
				t.Fatalf("blur = %v, want 6", op.Blur)
			}
			if op.Rect[2] != 200 || op.Rect[3] != 100 {
				t.Fatalf("backdrop box = %v, want 200x100", op.Rect)
			}
			if op.Radius[0] != 12 {
				t.Fatalf("backdrop radius = %v, want 12", op.Radius[0])
			}
		}
		if op.Kind == Fill && op.Color == 0x00000044 {
			fill = i
		}
	}
	if backdrop < 0 {
		t.Fatal("no backdrop op for backdropBlur: 6")
	}
	if fill < 0 {
		t.Fatal("the translucent background did not paint")
	}
	if backdrop > fill {
		t.Fatalf("backdrop op %d painted over the background fill %d", backdrop, fill)
	}
}

// TestNoBackdropBlurDrawsNothing keeps the default free: a design that never
// names the property must not pay for an extra render target.
func TestNoBackdropBlurDrawsNothing(t *testing.T) {
	for _, op := range list(t, `Window { Column { background: #ff0000 } }`, 400, 300).Ops {
		if op.Kind == Backdrop {
			t.Fatal("an element without backdropBlur emitted a backdrop op")
		}
	}
}
