package text

import (
	"testing"
)

func TestDefaultFontLoads(t *testing.T) {
	l := NewLibrary()
	if !l.HasFont() {
		t.Fatal("embedded font failed to parse")
	}
}

func TestMeasureTrailingTurkish(t *testing.T) {
	l := NewLibrary()
	f := l.Face(32, WeightRegular)
	if f == nil {
		t.Fatal("no face")
	}
	// Turkish characters must produce a non-zero advance and not panic.
	w, h := f.Measure("Çağrı İstanbul ışığı", -1)
	if w <= 0 || h <= 0 {
		t.Fatalf("measure = %v %v", w, h)
	}
	if h < 30 || h > 50 {
		t.Fatalf("line height = %v, expected about 38", h)
	}
}

func TestMeasureGrowsWithText(t *testing.T) {
	l := NewLibrary()
	f := l.Face(16, WeightRegular)
	short, _ := f.Measure("a", -1)
	long, _ := f.Measure("aaaa", -1)
	if long <= short {
		t.Fatalf("long = %v, short = %v", long, short)
	}
}

func TestMeasureEmpty(t *testing.T) {
	l := NewLibrary()
	f := l.Face(16, WeightRegular)
	w, _ := f.Measure("", -1)
	if w != 0 {
		t.Fatalf("empty width = %v", w)
	}
}

func TestWrapClampsWidth(t *testing.T) {
	l := NewLibrary()
	f := l.Face(16, WeightRegular)
	w, h := f.Measure("the quick brown fox jumps over the lazy dog", 100)
	if w > 100.01 {
		t.Fatalf("wrapped width = %v", w)
	}
	if h <= f.LineHeight() {
		t.Fatalf("wrapped height = %v, expected more than one line", h)
	}
}

func TestRasterGlyphHasInk(t *testing.T) {
	l := NewLibrary()
	f := l.Face(24, WeightRegular)
	out := f.Shape("A")
	if len(out.Glyphs) == 0 {
		t.Fatal("no glyphs")
	}
	img, off, ok := f.Raster(uint16(out.Glyphs[0].GlyphID))
	if !ok {
		t.Fatal("raster failed")
	}
	if img.Rect.Dx() == 0 || img.Rect.Dy() == 0 {
		t.Fatalf("empty bitmap %v", img.Rect)
	}
	ink := 0
	for _, v := range img.Pix {
		if v > 0 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("bitmap has no ink")
	}
	if img.Rect.Dx() < 8 || img.Rect.Dy() < 8 {
		t.Fatalf("glyph bitmap too small: %v", img.Rect)
	}
	_ = off
}

func TestAtlasCachesAndPacks(t *testing.T) {
	l := NewLibrary()
	f := l.Face(20, WeightRegular)
	a := l.Atlas()
	out := f.Shape("Hello")
	seen := 0
	for _, g := range out.Glyphs {
		slot, ok := a.Slot(f, uint16(g.GlyphID))
		if !ok {
			continue
		}
		seen++
		if slot.Rect.Empty() {
			t.Fatal("empty slot")
		}
		// Second call must return the same slot without touching the atlas.
		again, ok2 := a.Slot(f, uint16(g.GlyphID))
		if !ok2 || again.Rect != slot.Rect {
			t.Fatal("atlas did not cache the glyph")
		}
	}
	if seen == 0 {
		t.Fatal("no glyphs packed")
	}
	if a.Dirty.Empty() {
		t.Fatal("dirty region not recorded")
	}
}

func TestAtlasFullIsGraceful(t *testing.T) {
	l := NewLibrary()
	f := l.Face(40, WeightRegular)
	small := NewAtlas(16, 16)
	slots := 0
	for _, r := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		out := f.Shape(string(r))
		if len(out.Glyphs) == 0 {
			continue
		}
		if _, ok := small.Slot(f, uint16(out.Glyphs[0].GlyphID)); ok {
			slots++
		}
	}
	if slots > 8 {
		t.Fatalf("atlas accepted %d glyphs in 16x16", slots)
	}
}
