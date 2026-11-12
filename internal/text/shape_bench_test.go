package text

import (
	"fmt"
	"testing"
)

// shapeCases is a page worth of distinct runs, plus one repeated: the second
// benchmark is the loop the renderer actually runs, and the first is what it
// would cost without the cache.
func shapeStrings(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("Card %d — supporting line of text", i)
	}
	return out
}

func BenchmarkShapeDistinct(b *testing.B) {
	lib := NewLibrary()
	f := lib.Face(14, WeightRegular)
	strs := shapeStrings(maxShapedRuns + 1)
	b.ReportAllocs()
	b.ResetTimer()
	// One more than the cache holds, so every iteration really shapes.
	for i := 0; i < b.N; i++ {
		s := strs[i%len(strs)]
		f.shaped = nil
		f.Shape(s)
	}
}

func BenchmarkShapeCached(b *testing.B) {
	lib := NewLibrary()
	f := lib.Face(14, WeightRegular)
	strs := shapeStrings(maxShapedRuns)
	for _, s := range strs {
		f.Shape(s)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Shape(strs[i%len(strs)])
	}
}

// TestShapeIsCachedAndStable guards the contract the renderer relies on: the
// cached output is the same one a fresh shape would produce, and callers only
// read it.
func TestShapeIsCachedAndStable(t *testing.T) {
	lib := NewLibrary()
	f := lib.Face(14, WeightRegular)
	first := f.Shape("Hello world")
	second := f.Shape("Hello world")
	if first.Advance != second.Advance || len(first.Glyphs) != len(second.Glyphs) {
		t.Fatalf("cached shape differs: %+v vs %+v", first.Advance, second.Advance)
	}
	if len(first.Glyphs) == 0 {
		t.Fatal("no glyphs")
	}
	fresh := lib.Face(14, WeightRegular)
	fresh.shaped = nil
	again := fresh.shape("Hello world")
	if again.Advance != first.Advance || len(again.Glyphs) != len(first.Glyphs) {
		t.Fatalf("cached shape differs from a fresh one")
	}
}

func BenchmarkShapeRunsDistinct(b *testing.B) {
	lib := NewLibrary()
	f := lib.Face(14, WeightRegular)
	strs := shapeStrings(maxShapedRuns + 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := strs[i%len(strs)]
		f.runs = nil
		f.ShapeRuns(s)
	}
}

func BenchmarkShapeRunsCached(b *testing.B) {
	lib := NewLibrary()
	f := lib.Face(14, WeightRegular)
	strs := shapeStrings(maxShapedRuns)
	for _, s := range strs {
		f.ShapeRuns(s)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.ShapeRuns(strs[i%len(strs)])
	}
}

// TestShapeRunsIsCached guards the repaint path: the renderer asks for the same
// run every frame, and a second ask must not shape again.
func TestShapeRunsIsCached(t *testing.T) {
	lib := NewLibrary()
	f := lib.Face(14, WeightRegular)
	first := f.ShapeRuns("Hello world")
	second := f.ShapeRuns("Hello world")
	if len(first) != len(second) || &first[0].Output.Glyphs[0] != &second[0].Output.Glyphs[0] {
		t.Fatal("ShapeRuns shaped the same line twice")
	}
	// A different face keeps its own cache, so a different size cannot draw the
	// wrong glyphs.
	other := lib.Face(20, WeightRegular)
	if got := other.ShapeRuns("Hello world"); &got[0].Output.Glyphs[0] == &first[0].Output.Glyphs[0] {
		t.Fatal("two faces shared one shaping result")
	}
}
