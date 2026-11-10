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
	at := newTestImageRenderer(size)
	d := &img.Data{W: 8, H: 8, Pix: make([]byte, 8*8*4)}
	list := &paint.List{Ops: []paint.Op{{Kind: paint.Bitmap, Src: d, Rect: [4]float32{0, 0, 8, 8}}}}
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

// newTestImageRenderer is an atlas with no GPU, at 1x, for the packing logic.
func newTestImageRenderer(size int) *imageRenderer {
	return &imageRenderer{
		w: size, h: size,
		pix:     make([]byte, size*size*4),
		shelf:   img.Shelf{W: size, H: size},
		regions: map[imageKey]imageSlot{},
		gpu:     &GPU{scale: 1},
	}
}

// TestSmallImageIsPackedAtItsDrawnSize is the graininess guard: a 512px picture
// drawn at 30dp must not be minified 17x by a bilinear sampler, which is what
// made small images look rough. It is box-filtered down to about twice the drawn
// size instead.
func TestSmallImageIsPackedAtItsDrawnSize(t *testing.T) {
	at := newTestImageRenderer(1024)
	d := &img.Data{W: 512, H: 512, Pix: make([]byte, 512*512*4)}
	op := paint.Op{Kind: paint.Bitmap, Src: d, Rect: [4]float32{0, 0, 30, 30}}
	list := &paint.List{Ops: []paint.Op{op}}
	at.prepare(list)

	key := at.keyFor(op)
	if key.w != 64 || key.h != 64 {
		t.Fatalf("packed at %dx%d, want 64x64 (2x the drawn 30dp)", key.w, key.h)
	}
	slot, ok := at.regions[key]
	if !ok || slot.r.W != 64 {
		t.Fatalf("region = %+v, ok = %v", slot.r, ok)
	}

	// A bigger copy of the same picture keeps its own, sharper entry instead of
	// reusing the small one.
	big := paint.Op{Kind: paint.Bitmap, Src: d, Rect: [4]float32{0, 0, 256, 256}}
	at.prepare(&paint.List{Ops: []paint.Op{big}})
	if got := at.keyFor(big).w; got != 512 {
		t.Fatalf("large entry packed at %d, want the source's 512", got)
	}
	if len(at.regions) != 2 {
		t.Fatalf("regions = %d, want one per drawn size", len(at.regions))
	}

	// Twice the device scale halves the logical size, so a 30dp picture at 2x
	// still packs at 2x its device pixels.
	at.gpu.scale = 2
	if got := at.keyFor(op).w; got != 128 {
		t.Fatalf("at 2x packed at %d, want 128", got)
	}

	// A picture smaller than its quad is never upscaled: the sampler does that.
	small := &img.Data{W: 16, H: 16, Pix: make([]byte, 16*16*4)}
	at.gpu.scale = 1
	if got := at.keyForSize(small, 100, 100).w; got != 16 {
		t.Fatalf("small source packed at %d, want its own 16", got)
	}
}

// TestPackedImageReusesItsEntry guards a repack every frame: an image that did
// not fit at its requested size is filed under that request too, so the next
// frame finds it instead of scaling and copying it again.
func TestPackedImageReusesItsEntry(t *testing.T) {
	at := newTestImageRenderer(64)
	d := &img.Data{W: 200, H: 200, Pix: make([]byte, 200*200*4)}
	op := paint.Op{Kind: paint.Bitmap, Src: d, Rect: [4]float32{0, 0, 100, 100}}
	// Ask for a size the tiny atlas cannot hold, so pack shrinks it.
	key := at.keyFor(op)
	if _, ok := at.pack(d, key); !ok {
		t.Fatal("pack failed")
	}
	before := len(at.regions)
	at.prepare(&paint.List{Ops: []paint.Op{op}})
	if len(at.regions) != before {
		t.Fatalf("regions grew from %d to %d: the shrunken copy was not reused", before, len(at.regions))
	}
}
