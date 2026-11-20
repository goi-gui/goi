package tree

import "testing"

// TestBackdropBlurValues: the old number still reads as a radius, while a
// gradient keeps its peak for the scalar getter and turns into an alpha ramp
// for the renderer.
func TestBackdropBlurValues(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window { Column { backdropBlur: 6 } }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	col := tr.Root.Children()[0].Base()
	if got := col.BackdropBlur(); got != 6 {
		t.Fatalf("scalar = %v", got)
	}
	col.SetBackdropBlur(8)
	if got := col.BackdropBlur(); got != 8 {
		t.Fatalf("set = %v", got)
	}

	tr, diag = BuildSource([]byte(`Window { Column { backdropBlur: linear-gradient(90deg, 0 0%, 6 60%, 0 100%) } }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	b := tr.Root.Children()[0].Base().BackdropBlurValue()
	if b.Gradient == nil || b.Radius != 6 {
		t.Fatalf("gradient = %+v", b)
	}
	mask, peak := b.Mask(360, 200)
	if peak != 6 || mask == nil || len(mask.Stops) != 3 {
		t.Fatalf("mask = %+v peak = %v", mask, peak)
	}
	if mask.Stops[0].Color&0xFF != 0 || mask.Stops[1].Color&0xFF != 255 || mask.Stops[2].Color&0xFF != 0 {
		t.Fatalf("mask alphas = %08x %08x %08x", mask.Stops[0].Color, mask.Stops[1].Color, mask.Stops[2].Color)
	}
	if mask.Stops[1].At != 0.6 {
		t.Fatalf("mask stop = %v", mask.Stops[1].At)
	}
}

// TestBackdropBlurDPStops: a length position is resolved against the box the
// gradient paints.
func TestBackdropBlurDPStops(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window { Column { backdropBlur: linear-gradient(90deg, 0 0, 6 180, 0 360) } }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	mask, _ := tr.Root.Children()[0].Base().BackdropBlurValue().Mask(360, 200)
	if mask == nil || mask.Stops[1].At != 0.5 || mask.Stops[2].At != 1 {
		t.Fatalf("dp mask = %+v", mask)
	}
}
