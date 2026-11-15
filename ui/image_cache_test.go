package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/img"
)

// TestCapDecodedShrinksTheSource keeps a 12-megapixel photo from holding 49 MB
// of RGBA for a box that shows a few hundred pixels.
func TestCapDecodedShrinksTheSource(t *testing.T) {
	big := &img.Data{W: 4288, H: 2848, Pix: make([]byte, 16)}
	got := capDecoded(big)
	if got == big {
		t.Fatal("the oversized bitmap was not capped")
	}
	if got.W != 2048 || got.H != 1360 {
		t.Fatalf("capped size = %dx%d, want 2048x1360", got.W, got.H)
	}
	small := &img.Data{W: 640, H: 480}
	if capDecoded(small) != small {
		t.Fatal("a bitmap within the cap was copied")
	}
}

// TestImageCacheEvictsTheOldest covers the budget: a third picture over a
// two-picture budget drops the first, and the count follows.
func TestImageCacheEvictsTheOldest(t *testing.T) {
	a := &App{imageBudget: 2 * 4}
	for _, src := range []string{"a", "b"} {
		a.cacheImage(src, &img.Data{W: 1, H: 1, Pix: make([]byte, 4)}, nil)
	}
	if len(a.images) != 2 {
		t.Fatalf("cache holds %d pictures, want 2", len(a.images))
	}
	a.cacheImage("c", &img.Data{W: 1, H: 1, Pix: make([]byte, 4)}, nil)
	if _, ok := a.images["a"]; ok {
		t.Fatal("the oldest picture stayed in the cache")
	}
	if len(a.images) != 2 || a.imageBytes != 8 {
		t.Fatalf("cache = %d pictures, %d bytes", len(a.images), a.imageBytes)
	}
}
