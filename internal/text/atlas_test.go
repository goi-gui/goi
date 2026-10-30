package text

import "testing"

// atlasGlyphs is a spread of codepoints so each Slot is a distinct glyph.
var atlasGlyphs = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") // 62

// TestAtlasCompactsWhenFull is the point of compaction: a small atlas that runs
// out of shelves must keep serving glyphs instead of silently skipping them.
func TestAtlasCompactsWhenFull(t *testing.T) {
	lib := NewLibrary()
	face := lib.Face(16, WeightRegular)
	if face == nil {
		t.Skip("no font")
	}
	a := NewAtlas(64, 64)
	face.adv = nil
	for _, r := range atlasGlyphs {
		gid, ok := face.GlyphID(r)
		if !ok {
			continue
		}
		a.BeginFrame()
		if _, ok := a.Slot(face, gid); !ok {
			t.Fatalf("glyph %q was dropped from a 64x64 atlas", r)
		}
	}
	if a.Compactions == 0 && a.W == 64 && a.H == 64 {
		t.Fatal("expected the atlas to compact or grow")
	}
}

// TestAtlasCompactKeepsTheWorkingSet checks the safe half: glyphs requested in
// the current frame survive a rebuild, so instances already emitted stay valid.
func TestAtlasCompactKeepsTheWorkingSet(t *testing.T) {
	lib := NewLibrary()
	face := lib.Face(20, WeightRegular)
	if face == nil {
		t.Skip("no font")
	}
	a := NewAtlas(48, 48)
	// Fill the atlas with older frames so a compaction has something to drop.
	for round := 0; round < 3; round++ {
		a.BeginFrame()
		for _, r := range atlasGlyphs {
			if gid, ok := face.GlyphID(r); ok {
				a.Slot(face, gid)
			}
		}
	}
	// A fresh frame asks for a handful, then forces a rebuild by asking for all.
	a.BeginFrame()
	want := []rune("abcdef")
	for _, r := range want {
		gid, ok := face.GlyphID(r)
		if !ok {
			continue
		}
		a.Slot(face, gid)
	}
	for _, r := range atlasGlyphs {
		if gid, ok := face.GlyphID(r); ok {
			a.Slot(face, gid)
		}
	}
	for _, r := range want {
		gid, ok := face.GlyphID(r)
		if !ok {
			continue
		}
		slot, ok := a.Slot(face, gid)
		if !ok || slot.Rect.Empty() {
			t.Fatalf("working-set glyph %q lost after compaction/growth", r)
		}
	}
}

// TestAtlasGrowsRatherThanDropping covers the large-working-set path: when a
// single frame needs more room than the bitmap has, it grows.
func TestAtlasGrowsRatherThanDropping(t *testing.T) {
	lib := NewLibrary()
	face := lib.Face(32, WeightRegular)
	if face == nil {
		t.Skip("no font")
	}
	a := NewAtlas(32, 32)
	a.BeginFrame()
	for _, r := range atlasGlyphs {
		gid, ok := face.GlyphID(r)
		if !ok {
			continue
		}
		if _, ok := a.Slot(face, gid); !ok {
			t.Fatalf("glyph %q dropped instead of growing the atlas", r)
		}
	}
	if a.W <= 32 && a.H <= 32 {
		t.Fatalf("atlas stayed %dx%d for a %d-glyph working set", a.W, a.H, len(atlasGlyphs))
	}
	if !a.Resized {
		t.Fatal("growth did not ask the renderer to rebuild its texture")
	}
}
