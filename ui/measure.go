package ui

import "github.com/ksckaan1/goi/internal/text"

// textMeasurer adapts the text library to layout.Measurer so layout uses real
// glyph advances instead of the fake approximation.
type textMeasurer struct {
	lib *text.Library
}

// Measure implements layout.Measurer.
func (m textMeasurer) Measure(s string, fontSize, maxWidth float64) (float64, float64) {
	if m.lib == nil {
		// No font (headless tests without one): fall back to a stable estimate.
		return fakeMeasure(s, fontSize, maxWidth)
	}
	f := m.lib.Face(fontSize, text.Weight(400))
	if f == nil {
		return fakeMeasure(s, fontSize, maxWidth)
	}
	return f.Measure(s, maxWidth)
}

// MeasureWeight implements layout.Measurer with a real weight.
func (m textMeasurer) MeasureWeight(s string, fontSize float64, weight int) (float64, float64) {
	if m.lib == nil {
		return fakeMeasure(s, fontSize, -1)
	}
	f := m.lib.Face(fontSize, text.Weight(weight))
	if f == nil {
		return fakeMeasure(s, fontSize, -1)
	}
	return f.Measure(s, -1)
}

func fakeMeasure(s string, fontSize, maxWidth float64) (float64, float64) {
	w := float64(len(s)) * fontSize * 0.5
	if maxWidth > 0 && w > maxWidth {
		w = maxWidth
	}
	return w, fontSize * 1.2
}
