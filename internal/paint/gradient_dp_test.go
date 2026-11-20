package paint

import "testing"

// TestBackgroundGradientDPStops resolves a dp stop against the box that paints
// the gradient, with the same axis the shader uses.
func TestBackgroundGradientDPStops(t *testing.T) {
	l := list(t, `Window {
		Column {
			width: 200
			height: 100
			background: linear-gradient(90deg, #ff0000 0, #0000ff 100)
		}
	}`, 400, 300)
	for _, op := range l.Ops {
		if op.Shape != CSSGradient || op.Gradient == nil {
			continue
		}
		if op.Gradient.Stops[1].At != 0.5 {
			t.Fatalf("stop = %v, want 0.5", op.Gradient.Stops[1].At)
		}
		return
	}
	t.Fatal("no CSS gradient op")
}
