package tree

import "github.com/goi-gui/goi/internal/csscolor"

// BackdropBlur is the `backdropBlur` value: one radius in dp, or a gradient of
// radii. Radius is the peak of the gradient, so the scalar getter keeps working.
type BackdropBlur struct {
	Radius   float64
	Gradient *csscolor.ValueGradient
}

// Mask resolves a gradient blur for a box. It returns the alpha ramp between the
// unblurred scene and a fully blurred copy, and the peak radius the renderer
// blurs with. A scalar blur has no mask.
func (b BackdropBlur) Mask(w, h float64) (*Gradient, float64) {
	if b.Gradient == nil {
		return nil, b.Radius
	}
	peak := b.Gradient.Max()
	if peak <= 0 {
		return nil, 0
	}
	resolved := b.Gradient.Resolve(w, h)
	stops := make([]GradientStop, len(resolved))
	for i, st := range resolved {
		alpha := uint32(st.Value/peak*255 + 0.5)
		if alpha > 255 {
			alpha = 255
		}
		stops[i] = GradientStop{At: st.At, Color: 0xFFFFFF00 | alpha}
	}
	return &Gradient{Kind: b.Gradient.Kind, Angle: b.Gradient.Angle, Stops: stops}, peak
}

// sameBackdropBlur compares two values, including the gradient stops, so a
// rewritten design does not dirty the frame for an equal value.
func sameBackdropBlur(a, b BackdropBlur) bool {
	if a.Radius != b.Radius || (a.Gradient == nil) != (b.Gradient == nil) {
		return false
	}
	if a.Gradient == nil {
		return true
	}
	x, y := a.Gradient, b.Gradient
	if x.Kind != y.Kind || x.Angle != y.Angle || len(x.Stops) != len(y.Stops) {
		return false
	}
	for i := range x.Stops {
		if x.Stops[i] != y.Stops[i] {
			return false
		}
	}
	return true
}
