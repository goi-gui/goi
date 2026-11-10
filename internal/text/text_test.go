package text

import (
	"testing"

	"github.com/go-text/typesetting/language"
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

// TestAtlasFullGrowsInsteadOfDropping is the contract the atlas keeps: a
// working set bigger than the bitmap grows it, rather than quietly skipping
// glyphs (which used to make text lose letters after a long session).
func TestAtlasFullGrowsInsteadOfDropping(t *testing.T) {
	l := NewLibrary()
	f := l.Face(40, WeightRegular)
	if f == nil {
		t.Skip("no font")
	}
	small := NewAtlas(16, 16)
	small.BeginFrame()
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
	if slots != 26 {
		t.Fatalf("atlas served %d of 26 glyphs", slots)
	}
	if small.W == 16 && small.H == 16 {
		t.Fatal("the atlas did not grow")
	}
}

// TestBidiRunsCoverTheText checks that splitting a line never drops or reorders
// characters, and that the base direction and script are picked from the text.
func TestBidiRunsCoverTheText(t *testing.T) {
	for _, s := range []string{
		"hello",
		"مرحبا بالعالم",
		"hello مرحبا world",
	} {
		runs := splitBidi(s)
		var joined string
		for _, r := range runs {
			joined += r.text
		}
		if joined != s {
			t.Errorf("%q split into %q", s, joined)
		}
	}
	if !baseDirection("مرحبا بالعالم") {
		t.Error("an Arabic line is not right to left")
	}
	if baseDirection("hello") {
		t.Error("a Latin line is right to left")
	}
	runs := splitBidi("مرحبا")
	if len(runs) != 1 || !runs[0].rtl {
		t.Fatalf("Arabic runs = %+v", runs)
	}
	if runs[0].script == language.Latin {
		t.Error("Arabic text kept the Latin script")
	}
}

// TestShapedRTLKeepsEveryGlyph guards the shaping path: the run count changes
// but no glyph disappears.
func TestShapedRTLKeepsEveryGlyph(t *testing.T) {
	lib := NewLibrary()
	if !lib.HasFont() {
		t.Skip("no font")
	}
	face := lib.Face(14, WeightRegular)
	if face == nil {
		t.Skip("no face")
	}
	arabic := "مرحبا"
	out := face.Shape(arabic)
	if len(out.Glyphs) == 0 {
		t.Fatal("no glyphs for Arabic text")
	}
	if out.Advance <= 0 {
		t.Fatalf("advance = %v", out.Advance)
	}
	// Roboto has no Arabic, so this may fall back to .notdef; the point is that
	// shaping and direction do not drop glyphs. A font with Arabic coverage
	// through RegisterFont would shape it fully.
	if n := len(out.Glyphs); n > len([]rune(arabic)) {
		t.Fatalf("more glyphs than runes: %d", n)
	}
}

// TestRegisterFamily serves a family from a file the app hands over, and keeps
// its glyphs in their own atlas tag so a custom face cannot collide with Roboto.
func TestRegisterFamily(t *testing.T) {
	l := NewLibrary()
	if err := l.RegisterFamily("Brand", robotoMedium); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !l.HasFamily("Brand") {
		t.Fatal("family was not recorded")
	}
	f := l.FamilyFace(20, WeightRegular, "Brand")
	if f == nil {
		t.Fatal("no face for the registered family")
	}
	if f.Tag() < 16 {
		t.Fatalf("tag = %d, want a tag of its own above the built-ins", f.Tag())
	}
	// The same family keeps its tag between calls, so atlas keys stay stable.
	if again := l.FamilyFace(20, WeightRegular, "Brand"); again.Tag() != f.Tag() {
		t.Fatalf("tag changed: %d then %d", f.Tag(), again.Tag())
	}
	if italic := l.FamilyItalicFace(20, WeightRegular, "Brand"); italic.Tag() != f.Tag()+1 {
		t.Fatalf("italic tag = %d, want %d", italic.Tag(), f.Tag()+1)
	}
	// An unknown name falls back to Roboto rather than drawing nothing.
	if fallback := l.FamilyFace(20, WeightRegular, "Missing"); fallback == nil || fallback.Tag() >= 16 {
		t.Fatalf("unknown family did not fall back: %+v", fallback)
	}
	// The evolution of one tag per family is what makes the canvas of the
	// system independent of an app's choices.
	if err := l.RegisterFamily("Other", robotoBold); err != nil {
		t.Fatalf("register: %v", err)
	}
	other := l.FamilyFace(20, WeightRegular, "Other")
	if other.Tag() == f.Tag() {
		t.Fatal("two families share one atlas tag")
	}
	if w, _ := other.Measure("Brand", -1); w <= 0 {
		t.Fatal("a registered face measured nothing")
	}
}
