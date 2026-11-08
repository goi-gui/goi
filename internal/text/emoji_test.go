package text

import "testing"

// TestEmojiFallsBack checks that a rune Roboto has no glyph for is shaped with
// the colour face, while text around it stays on Roboto.
func TestEmojiFallsBack(t *testing.T) {
	l := NewLibrary()
	if !l.HasEmoji() {
		t.Skip("no emoji font embedded")
	}
	f := l.Face(16, WeightRegular)
	runs := f.ShapeRuns("Hi 😀 there")
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want text, emoji, text", len(runs))
	}
	if runs[0].Face.IsEmoji() || runs[2].Face.IsEmoji() {
		t.Fatal("plain text went to the emoji face")
	}
	if !runs[1].Face.IsEmoji() {
		t.Fatal("the emoji did not fall back")
	}
	if len(runs[1].Output.Glyphs) != 1 {
		t.Fatalf("emoji glyphs = %d, want 1", len(runs[1].Output.Glyphs))
	}
}

// TestEmojiSequenceStaysOneGlyph covers a ZWJ sequence: it must shape to one
// glyph, not a string of separate people.
func TestEmojiSequenceStaysOneGlyph(t *testing.T) {
	l := NewLibrary()
	if !l.HasEmoji() {
		t.Skip("no emoji font embedded")
	}
	f := l.Face(16, WeightRegular)
	for _, seq := range []string{"👨‍👩‍👧‍👦", "🏳️‍🌈", "🇹🇷", "👍🏽"} {
		runs := f.ShapeRuns(seq)
		if len(runs) != 1 || !runs[0].Face.IsEmoji() {
			t.Fatalf("%q shaped as %d runs", seq, len(runs))
		}
		if n := len(runs[0].Output.Glyphs); n != 1 {
			t.Fatalf("%q shaped to %d glyphs, want 1", seq, n)
		}
	}
}

// TestEmojiColorGlyph checks the bitmaps themselves: a colour image at the
// face's size, with pixels that are not all transparent.
func TestEmojiColorGlyph(t *testing.T) {
	l := NewLibrary()
	if !l.HasEmoji() {
		t.Skip("no emoji font embedded")
	}
	em := l.EmojiFace(24)
	gid, ok := em.GlyphID('😀')
	if !ok {
		t.Fatal("no glyph for a smiley")
	}
	img, off, ok := em.ColorGlyph(gid)
	if !ok {
		t.Fatal("no colour bitmap")
	}
	if img.Bounds().Dx() < 8 || img.Bounds().Dy() < 8 {
		t.Fatalf("bitmap is %v", img.Bounds())
	}
	opaque := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 128 {
			opaque++
		}
	}
	if opaque == 0 {
		t.Fatal("the colour glyph decoded fully transparent")
	}
	if off.Y >= 0 {
		t.Fatalf("offset %v puts the emoji below the baseline", off)
	}
}

// TestEmojiMeasureMatchesRuns keeps layout and paint honest: the width a wrap
// measures is the width the shaped runs draw.
func TestEmojiMeasureMatchesRuns(t *testing.T) {
	l := NewLibrary()
	if !l.HasEmoji() {
		t.Skip("no emoji font embedded")
	}
	f := l.Face(16, WeightRegular)
	s := "a😀b"
	want := 0.0
	for _, r := range f.ShapeRuns(s) {
		want += float64(r.Output.Advance) / 64
	}
	if got := f.advance(s); got != want {
		t.Fatalf("advance = %v, shaped runs = %v", got, want)
	}
}
