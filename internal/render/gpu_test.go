//go:build !nogpu

package render

import (
	"testing"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/paint"
)

// TestClearColorFollowsTheme guards dark mode: the page behind the components is
// the theme surface, so a light-mode clear would leave a white sheet under a dark
// UI.
func TestClearColorFollowsTheme(t *testing.T) {
	light := &paint.List{Theme: m3.Light()}
	dark := &paint.List{Theme: m3.Dark()}
	if got := clearColor(light); got.R < 0.9 || got.G < 0.9 {
		t.Fatalf("light clear = %+v, want the light surface", got)
	}
	got := clearColor(dark)
	if got.R > 0.2 || got.G > 0.2 || got.B > 0.2 {
		t.Fatalf("dark clear = %+v, want a dark surface", got)
	}
	if got := clearColor(nil); got.A != 1 {
		t.Fatalf("nil list clear = %+v", got)
	}
}

func TestSnapRect(t *testing.T) {
	// A 1dp border on a fractional box used to straddle two rows; snapped, both
	// edges land on whole device pixels.
	got := snapRect([4]float32{196.23438, 113.2, 100.390625, 40}, 1)
	want := [4]float32{196, 113, 101, 40}
	if got != want {
		t.Fatalf("scale 1: got %v want %v", got, want)
	}

	// Edges snap from their absolute position, so neighbouring boxes stay flush.
	a := snapRect([4]float32{10.4, 0, 20, 10}, 1)
	b := snapRect([4]float32{30.4, 0, 20, 10}, 1)
	if a[0]+a[2] != b[0] {
		t.Fatalf("seam: %v then %v", a, b)
	}

	// At 2x the grid is half a logical pixel.
	got = snapRect([4]float32{10.1, 10.3, 5.6, 5}, 2)
	want = [4]float32{10, 10.5, 5.5, 5}
	if got != want {
		t.Fatalf("scale 2: got %v want %v", got, want)
	}
}

// TestImageAtlasUpdatesInPlace is the regression test for an animation that
// filled the atlas: a frame whose generation changed must reuse its region, not
// pack a new one.
func TestImageAtlasUpdatesInPlace(t *testing.T) {
	const size = 64
	at := &imageRenderer{
		w: size, h: size,
		pix:     make([]byte, size*size*4),
		shelf:   img.Shelf{W: size, H: size},
		regions: map[*img.Data]img.Region{},
		gens:    map[*img.Data]int{},
	}
	d := &img.Data{W: 8, H: 8, Pix: make([]byte, 8*8*4)}
	list := &paint.List{Ops: []paint.Op{{Kind: paint.Bitmap, Src: d}}}
	at.prepare(list)
	if len(at.regions) != 1 {
		t.Fatalf("regions after the first frame = %d", len(at.regions))
	}
	for i := 0; i < 50; i++ {
		d.Pix[0] = byte(i)
		d.Gen++
		at.prepare(list)
	}
	if len(at.regions) != 1 {
		t.Fatalf("regions after 50 frames = %d, want the one the image already owns", len(at.regions))
	}
	if !at.dirty {
		t.Fatal("the new frame was not marked for upload")
	}
}
