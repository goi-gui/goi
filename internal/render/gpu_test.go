//go:build !nogpu

package render

import "testing"

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
