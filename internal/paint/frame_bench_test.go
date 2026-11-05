package paint

import (
	"os"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// realMeasurer measures with the embedded font, so a benchmark sees the shaping
// cost a real frame pays. FakeMeasurer would make every frame look cheap.
type realMeasurer struct{ lib *text.Library }

func (m realMeasurer) Measure(s string, size, maxW float64) (float64, float64) {
	f := m.lib.Face(size, text.Weight(400))
	return f.Measure(s, maxW)
}

func (m realMeasurer) MeasureWeight(s string, size float64, weight int) (float64, float64) {
	f := m.lib.Face(size, text.Weight(weight))
	return f.Measure(s, 0)
}

// galleryTree loads the gallery design, which is the widest real page: every
// component kind, nested containers and a lot of text. It lives in the sibling
// examples module, so this skips when that checkout is not next to this one.
func galleryTree(b *testing.B) *tree.Tree {
	b.Helper()
	src, err := os.ReadFile("../../../examples/gallery/gallery.goi")
	if err != nil {
		b.Skipf("gallery design not available: %v", err)
	}
	tr, diag := tree.BuildSource(src, nil)
	if !diag.Empty() {
		b.Fatal(diag)
	}
	return tr
}

// BenchmarkLayoutGallery is the relayout half of a dirty frame.
func BenchmarkLayoutGallery(b *testing.B) {
	tr := galleryTree(b)
	e := layout.New(realMeasurer{lib: text.NewLibrary()})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Layout(tr.Root, 1100, 900)
	}
}

// BenchmarkPaintGallery is what a hover or a ripple pays: the display list is
// rebuilt without a relayout.
func BenchmarkPaintGallery(b *testing.B) {
	tr := galleryTree(b)
	m := realMeasurer{lib: text.NewLibrary()}
	layout.New(m).Layout(tr.Root, 1100, 900)
	l := &List{}
	l.Measure = func(s string, size float64, weight int) float64 {
		w, _ := m.MeasureWeight(s, size, weight)
		return w
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Build(tr, 1100, 900)
	}
}

// BenchmarkFrameGallery is a full frame: layout plus display list.
func BenchmarkFrameGallery(b *testing.B) {
	tr := galleryTree(b)
	m := realMeasurer{lib: text.NewLibrary()}
	e := layout.New(m)
	l := &List{}
	l.Measure = func(s string, size float64, weight int) float64 {
		w, _ := m.MeasureWeight(s, size, weight)
		return w
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Layout(tr.Root, 1100, 900)
		l.Build(tr, 1100, 900)
	}
}
