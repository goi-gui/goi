package platform

import "testing"

// TestClampSize pins the arithmetic behind the window limits: a zero limit means
// "none", the limits are logical points scaled to pixels, and min wins over max
// when a design declares them crossed.
func TestClampSize(t *testing.T) {
	cases := []struct {
		name                   string
		minW, minH, maxW, maxH int
		scale                  float64
		inW, inH, wantW, wantH int
	}{
		{"inside", 420, 360, 1100, 900, 1, 800, 600, 800, 600},
		{"over max", 420, 360, 1100, 900, 1, 1600, 1200, 1100, 900},
		{"under min", 420, 360, 1100, 900, 1, 200, 150, 420, 360},
		{"only width", 420, 0, 1000, 0, 1, 200, 150, 420, 150},
		{"only height", 0, 360, 0, 900, 1, 200, 1500, 200, 900},
		{"no limits", 0, 0, 0, 0, 1, 1920, 1080, 1920, 1080},
		{"hidpi scales the limit", 400, 300, 800, 600, 2, 100, 100, 800, 600},
		{"hidpi max", 0, 0, 800, 600, 2, 4000, 4000, 1600, 1200},
	}
	for _, c := range cases {
		gotW, gotH := clampSize(c.minW, c.minH, c.maxW, c.maxH, c.scale, c.inW, c.inH)
		if gotW != c.wantW || gotH != c.wantH {
			t.Errorf("%s: clampSize = %dx%d, want %dx%d", c.name, gotW, gotH, c.wantW, c.wantH)
		}
	}
}
