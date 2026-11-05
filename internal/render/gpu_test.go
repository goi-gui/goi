//go:build !nogpu

package render

import (
	"testing"

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
