package paint

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestImageLoadingPlaceholder covers the frame before a remote bitmap arrives:
// no bitmap op, but a wash whose opacity follows Phase, so the space reads as
// loading instead of empty.
func TestImageLoadingPlaceholder(t *testing.T) {
	build := func(loading bool, phase float64) (*List, bool, uint32) {
		tr, diag := tree.BuildSource([]byte(`Window { Column {
			Image { id: pic src: "https://example.com/a.png" width: 40 height: 20 }
		} }`), nil)
		if !diag.Empty() {
			t.Fatal(diag)
		}
		node, _ := tr.ByID("pic")
		im := node.(*tree.Image)
		im.Loading = loading
		im.Phase = phase
		layout.New(nil).Layout(tr.Root, 200, 200)
		l := &List{}
		l.Build(tr, 200, 200)
		var bitmap bool
		var wash uint32
		for _, op := range l.Ops {
			if op.Kind == Bitmap {
				bitmap = true
			}
			// The placeholder is the only fill in this tiny tree.
			if op.Kind == Fill && op.Color != 0 {
				wash = op.Color
			}
		}
		return l, bitmap, wash
	}

	// Not loading, no data: nothing beyond the node's own (absent) background.
	if _, bitmap, wash := build(false, 0); bitmap || wash != 0 {
		t.Fatalf("an empty static image drew something: bitmap=%v color=%08x", bitmap, wash)
	}

	l, bitmap, dim := build(true, 0)
	if bitmap {
		t.Fatal("the placeholder drew a bitmap")
	}
	if dim == 0 {
		t.Fatalf("no placeholder wash:\n%s", l.Dump())
	}
	_, _, bright := build(true, 1)
	if bright == dim {
		t.Fatalf("phase did not change the wash: both %08x", dim)
	}
	// The wash rides on the theme's onSurface, so it follows light and dark.
	if _, _, _ = build(true, 0.5); bright&0xFF <= dim&0xFF {
		t.Fatalf("the wash did not brighten with phase: %08x -> %08x", dim, bright)
	}
}

// TestOffscreenBitmapIsOmitted keeps a picture outside the clip out of the
// display list: packing every off-screen image filled the atlas and made the
// visible ones blurry, especially while a Mermaid page scrolled.
func TestOffscreenBitmapIsOmitted(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Stack { Positioned {
		top: 500
		Image { id: pic src: "x" width: 40 height: 20 }
	} } }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	node, _ := tr.ByID("pic")
	im := node.(*tree.Image)
	im.Data = &img.Data{W: 1, H: 1, Pix: make([]byte, 4)}
	layout.New(nil).Layout(tr.Root, 100, 100)
	l := &List{}
	l.Build(tr, 100, 100)
	for _, op := range l.Ops {
		if op.Kind == Bitmap {
			t.Fatalf("an off-screen image emitted a bitmap: %+v", op.Rect)
		}
	}
}

// TestMermaidLoadingShimmers: a diagram waiting for its render draws a skeleton
// block and keeps the frame loop awake for the sweep.
func TestMermaidLoadingShimmers(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Mermaid { id: chart width: 200 height: 120 } }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	node, _ := tr.ByID("chart")
	m := node.(*tree.Mermaid)
	m.Loading = true
	layout.New(nil).Layout(tr.Root, 300, 200)
	l := &List{}
	l.Build(tr, 300, 200)
	if !l.Animating {
		t.Fatal("a loading diagram did not ask for the next frame")
	}
	var bands int
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Shape == CSSGradient && op.Gradient != nil {
			bands++
		}
	}
	if bands == 0 {
		t.Fatalf("no shimmer band:\n%s", l.Dump())
	}
}

// TestImageCrossfadePaintsBothBitmaps: halfway through a fade the old and new
// pictures are complementary alphas over the same box.
func TestImageCrossfadePaintsBothBitmaps(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Image {
		id:           pic
		src:          "x"
		crossfadeSrc: true
		width:        40
		height:       20
	} }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	node, _ := tr.ByID("pic")
	im := node.(*tree.Image)
	prev := &img.Data{W: 2, H: 2, Pix: make([]byte, 16)}
	im.Fade.Prev = prev
	im.Fade.Dur = 300 * time.Millisecond
	im.Fade.Start = time.Now().Add(-150 * time.Millisecond)
	im.Fade.Active = true
	im.Data = &img.Data{W: 2, H: 2, Pix: make([]byte, 16)}
	layout.New(nil).Layout(tr.Root, 100, 100)
	l := &List{}
	l.Build(tr, 100, 100)
	var alphas []uint32
	for _, op := range l.Ops {
		if op.Kind == Bitmap {
			alphas = append(alphas, op.Color&0xFF)
		}
	}
	if len(alphas) != 2 {
		t.Fatalf("bitmap ops = %d, want the old and new pictures", len(alphas))
	}
	for _, a := range alphas {
		if a < 100 || a > 155 {
			t.Fatalf("crossfade alphas = %v, want about half", alphas)
		}
	}
	if !l.Animating {
		t.Fatal("a running fade did not ask for the next frame")
	}
}
