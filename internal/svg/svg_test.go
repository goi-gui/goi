package svg

import (
	"testing"

	"github.com/goi-gui/goi/internal/img"
)

// testSVG is a 10x20 rectangle that follows currentColor, so a render shows
// both the substitution and the aspect.
const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 20">
	<rect width="10" height="20" fill="currentColor"/>
</svg>`

func TestParseIntrinsic(t *testing.T) {
	s, err := Parse([]byte(testSVG))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := s.Intrinsic(); w != 10 || h != 20 {
		t.Fatalf("intrinsic = %vx%v, want 10x20", w, h)
	}
}

func TestRenderReplacesCurrentColor(t *testing.T) {
	s, err := Parse([]byte(testSVG))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFF0000FF)
	if err != nil {
		t.Fatal(err)
	}
	if d.W != 40 || d.H != 80 {
		t.Fatalf("raster = %dx%d, want 40x80 (4x viewBox)", d.W, d.H)
	}
	r, g, b, a := pixelAt(d, d.W/2, d.H/2)
	if r < 240 || g > 15 || b > 15 || a < 250 {
		t.Fatalf("centre = %d,%d,%d,%d, want opaque red", r, g, b, a)
	}

	blue, err := s.Render(0x0000FFFF)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a = pixelAt(blue, blue.W/2, blue.H/2)
	if r > 15 || g > 15 || b < 240 || a < 250 {
		t.Fatalf("centre = %d,%d,%d,%d, want opaque blue", r, g, b, a)
	}
}

func TestRenderAppliesAlpha(t *testing.T) {
	s, err := Parse([]byte(testSVG))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFF000080)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(d, d.W/2, d.H/2); a < 120 || a > 135 {
		t.Fatalf("alpha = %d, want about 128", a)
	}
}

// TestRenderPath covers the shape real icons use, not just a rect.
func TestRenderPath(t *testing.T) {
	const star = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
		<path fill="currentColor" d="M12 2l2.9 6.3 6.9.8-5.1 4.7 1.4 6.8L12 17.3 5.9 20.6l1.4-6.8L2.2 9.1l6.9-.8z"/>
	</svg>`
	s, err := Parse([]byte(star))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0x00FF00FF)
	if err != nil {
		t.Fatal(err)
	}
	opaque := 0
	for i := 3; i < len(d.Pix); i += 4 {
		if d.Pix[i] > 0 {
			opaque++
		}
	}
	if opaque == 0 {
		t.Fatal("the path rendered nothing")
	}
}

func pixelAt(d *img.Data, x, y int) (r, g, b, a uint8) {
	i := (y*d.W + x) * 4
	return d.Pix[i], d.Pix[i+1], d.Pix[i+2], d.Pix[i+3]
}
