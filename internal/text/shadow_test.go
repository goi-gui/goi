package text

import "testing"

// TestShadowSlotIsBiggerAndSoft checks the two properties a blurred glyph slot
// must have: the bitmap grows by the blur's padding (with the offset moved by the
// same amount, so the glyph stays put) and the coverage reaches past the tight
// glyph box instead of ending on it.
func TestShadowSlotIsBiggerAndSoft(t *testing.T) {
	lib := NewLibrary()
	face := lib.Face(32, WeightRegular)
	if face == nil {
		t.Skip("no font")
	}
	gid, ok := face.GlyphID('o')
	if !ok {
		t.Skip("no glyph")
	}
	a := NewAtlas(512, 512)
	a.BeginFrame()
	sharp, ok := a.Slot(face, gid)
	if !ok {
		t.Fatal("no sharp slot")
	}
	soft, ok := a.ShadowSlot(face, gid, 6)
	if !ok {
		t.Fatal("no shadow slot")
	}
	if soft.Rect.Dx() <= sharp.Rect.Dx() || soft.Rect.Dy() <= sharp.Rect.Dy() {
		t.Fatalf("shadow slot %v is not bigger than %v", soft.Rect, sharp.Rect)
	}
	if soft.Offset.X >= sharp.Offset.X || soft.Offset.Y >= sharp.Offset.Y {
		t.Fatalf("shadow offset %v did not move out from %v", soft.Offset, sharp.Offset)
	}

	// A hard glyph edge is mostly empty inside its box; a blurred copy has
	// coverage in the padding the sharp one does not use.
	var softEdge int
	for y := soft.Rect.Min.Y; y < soft.Rect.Max.Y; y++ {
		for x := soft.Rect.Min.X; x < soft.Rect.Max.X; x++ {
			if a.Img.Pix[y*a.Img.Stride+x] > 0 {
				softEdge++
			}
		}
	}
	var sharpEdge int
	for y := sharp.Rect.Min.Y; y < sharp.Rect.Max.Y; y++ {
		for x := sharp.Rect.Min.X; x < sharp.Rect.Max.X; x++ {
			if a.Img.Pix[y*a.Img.Stride+x] > 0 {
				sharpEdge++
			}
		}
	}
	if softEdge <= sharpEdge {
		t.Fatalf("blurred coverage %d is not larger than sharp %d", softEdge, sharpEdge)
	}
}

// TestShadowSlotZeroIsTheGlyph keeps `blur: 0` a hard copy: it reuses the plain
// slot instead of paying for a softened bitmap.
func TestShadowSlotZeroIsTheGlyph(t *testing.T) {
	lib := NewLibrary()
	face := lib.Face(24, WeightRegular)
	if face == nil {
		t.Skip("no font")
	}
	gid, ok := face.GlyphID('A')
	if !ok {
		t.Skip("no glyph")
	}
	a := NewAtlas(256, 256)
	a.BeginFrame()
	sharp, _ := a.Slot(face, gid)
	soft, ok := a.ShadowSlot(face, gid, 0)
	if !ok || soft.Rect != sharp.Rect || soft.Offset != sharp.Offset {
		t.Fatalf("zero-blur slot %v/%v, want %v/%v", soft.Rect, soft.Offset, sharp.Rect, sharp.Offset)
	}
}
