package text

import (
	"image"
	"testing"
)

// square returns a closed contour. cw reverses the direction, which is how a
// real counter (the hole in an "o") is wound relative to its outside.
func square(x0, y0, x1, y1 float32, cw bool) [][2]float32 {
	if cw {
		return [][2]float32{{x0, y0}, {x0, y1}, {x1, y1}, {x1, y0}}
	}
	return [][2]float32{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

// coverageAt fills one scanline and returns the coverage of a pixel.
func coverageAt(contours [][][2]float32, y float32, width int, x int) float64 {
	acc := make([]float64, width)
	fillRow(scanline(contours, y), acc, 0, 1)
	return acc[x]
}

// TestOverlappingContoursFillSolid is the glyph bug: TrueType outlines overlap
// themselves where strokes meet (the stem and crossbar of a "t", the bowl and
// stem of an "R"). With an even-odd fill the overlap cancels into a hole, which
// shows up as white gaps inside the letter.
func TestOverlappingContoursFillSolid(t *testing.T) {
	outer := square(0, 0, 10, 10, false)
	inner := square(4, 4, 6, 6, false) // same direction: an overlap, not a counter
	contours := [][][2]float32{outer, inner}

	if got := coverageAt(contours, 5, 10, 5); got < 0.99 {
		t.Fatalf("overlap coverage = %v, want 1: the crossing area must fill", got)
	}
	// The rest of the outer square is unaffected.
	if got := coverageAt(contours, 5, 10, 1); got < 0.99 {
		t.Fatalf("outer coverage = %v, want 1", got)
	}
}

// TestOppositeContourCarvesACounter is the other half of the rule: a contour
// wound the other way is a hole, which is how "o" keeps its middle empty.
func TestOppositeContourCarvesACounter(t *testing.T) {
	outer := square(0, 0, 10, 10, false)
	hole := square(3, 3, 7, 7, true)
	contours := [][][2]float32{outer, hole}

	if got := coverageAt(contours, 5, 10, 5); got != 0 {
		t.Fatalf("counter coverage = %v, want 0", got)
	}
	if got := coverageAt(contours, 5, 10, 1); got < 0.99 {
		t.Fatalf("ring coverage = %v, want 1", got)
	}
}

// TestGlyphRasterHasNoEnclosedHoles is the end-to-end guard: rasterize letters
// whose strokes overlap, then look for a fully transparent pixel that is
// surrounded by ink on all four sides. A real counter ("o") is open to the
// outside or has its own enclosed area, so the check runs on "t" only, which
// has no counter at all.
func TestGlyphRasterHasNoEnclosedHoles(t *testing.T) {
	lib := NewLibrary()
	if !lib.HasFont() {
		t.Skip("no embedded font")
	}
	face := lib.Face(96, WeightRegular)
	gid, ok := face.GlyphID('t')
	if !ok {
		t.Fatal("no glyph for t")
	}
	bitmap, _, ok := face.Raster(gid)
	if !ok {
		t.Fatal("no raster")
	}
	for y := 1; y < bitmap.Bounds().Dy()-1; y++ {
		for x := 1; x < bitmap.Bounds().Dx()-1; x++ {
			if bitmap.GrayAt(x, y).Y != 0 {
				continue
			}
			if hasInk(bitmap, x, y, -1, 0) && hasInk(bitmap, x, y, 1, 0) &&
				hasInk(bitmap, x, y, 0, -1) && hasInk(bitmap, x, y, 0, 1) {
				t.Fatalf("transparent pixel at %d,%d is enclosed by ink: overlapping "+
					"contours were filled with an even-odd rule", x, y)
			}
		}
	}
}

// hasInk reports whether any pixel in that direction is opaque.
func hasInk(img image.Gray, x, y, dx, dy int) bool {
	b := img.Bounds()
	for x >= 0 && y >= 0 && x < b.Dx() && y < b.Dy() {
		if img.GrayAt(x, y).Y > 200 {
			return true
		}
		x += dx
		y += dy
	}
	return false
}
