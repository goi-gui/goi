package paint

import (
	"testing"

	"github.com/ksckaan1/goi/internal/layout"
	"github.com/ksckaan1/goi/internal/tree"
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
