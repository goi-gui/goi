package text

import "testing"

// TestFilledIconFaceIsTheFilledInstance guards the two embedded icon faces: the
// filled one keeps the same ligatures (a missing name would shape to the four
// letters) and draws a different outline for the same icon.
func TestFilledIconFaceIsTheFilledInstance(t *testing.T) {
	lib := NewLibrary()
	plain, filled := lib.IconFace(24), lib.FilledIconFace(24)
	if plain == nil || filled == nil {
		t.Fatal("an icon face did not load")
	}
	if plain == filled {
		t.Fatal("filledIcon got the outlined face")
	}

	glyph := func(f *Face, name string) (uint16, int) {
		n := 0
		var gid uint16
		for _, r := range f.ShapeRuns(name) {
			for _, g := range r.Output.Glyphs {
				gid = uint16(g.GlyphID)
				n++
			}
		}
		return gid, n
	}
	gid, n := glyph(filled, "home")
	if n != 1 {
		t.Fatalf("the filled face shaped `home` to %d glyphs, want the ligature", n)
	}
	pgid, pn := glyph(plain, "home")
	if pn != 1 {
		t.Fatalf("the outlined face shaped `home` to %d glyphs", pn)
	}
	a, _, oka := plain.Raster(pgid)
	b, _, okb := filled.Raster(gid)
	if !oka || !okb {
		t.Fatal("a glyph did not rasterize")
	}
	if len(a.Pix) != len(b.Pix) {
		t.Fatalf("rasters differ in size: %d vs %d", len(a.Pix), len(b.Pix))
	}
	same := true
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("the filled and outlined faces rasterized the same glyph")
	}
}
