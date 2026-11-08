package svg

import (
	"testing"
	"time"

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

// animSVG fades a rect from red to blue over one second and loops.
const animSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
	<rect width="10" height="10" fill="#ff0000">
		<animate attributeName="fill" from="#ff0000" to="#0000ff" dur="1s" repeatCount="indefinite"/>
	</rect>
</svg>`

// TestAnimatedSVGInterpolatesColours checks the SMIL fill path: the same file
// renders a different colour at the start, the middle and the end of its cycle.
func TestAnimatedSVGInterpolatesColours(t *testing.T) {
	s, err := Parse([]byte(animSVG))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Animated() || s.Duration() != time.Second {
		t.Fatalf("animated = %v, duration = %v", s.Animated(), s.Duration())
	}
	at := func(d time.Duration) (uint8, uint8, uint8) {
		d2, err := s.RenderAt(0xFFFFFFFF, d)
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, _ := pixelAt(d2, d2.W/2, d2.H/2)
		return r, g, b
	}
	if r, _, b := at(0); r < 240 || b > 15 {
		t.Fatalf("at 0 = %d,%d, want red", r, b)
	}
	if r, _, b := at(500 * time.Millisecond); r < 100 || r > 155 || b < 100 || b > 155 {
		t.Fatalf("at 500ms = %d,%d, want the mix", r, b)
	}
	if r, _, b := at(999 * time.Millisecond); r > 15 || b < 240 {
		t.Fatalf("at 999ms = %d,%d, want blue", r, b)
	}
	// Indefinite repeats wrap around.
	if r, _, b := at(1500 * time.Millisecond); r < 100 || r > 155 {
		t.Fatalf("at 1.5s = %d,%d, want it wrapped to the middle", r, b)
	}
}

// TestAnimatedSVGOpacity checks a numeric attribute and that the animation
// element itself is not drawn.
func TestAnimatedSVGOpacity(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
		<rect width="10" height="10" fill="#00ff00">
			<animate attributeName="opacity" from="1" to="0" dur="2s" repeatCount="indefinite"/>
		</rect>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	full, err := s.RenderAt(0xFFFFFFFF, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(full, full.W/2, full.H/2); a < 250 {
		t.Fatalf("alpha at 0 = %d, want opaque", a)
	}
	half, err := s.RenderAt(0xFFFFFFFF, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(half, half.W/2, half.H/2); a < 100 || a > 155 {
		t.Fatalf("alpha at 1s = %d, want about half", a)
	}
}

// TestAnimatedSVGTransform checks animateTransform: a rotation sweeps the shape
// across the canvas.
func TestAnimatedSVGTransform(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">
		<rect x="0" y="9" width="20" height="2" fill="#000000" transform="rotate(0 10 10)">
			<animateTransform attributeName="transform" type="rotate" from="0 10 10" to="360 10 10" dur="1s" repeatCount="indefinite"/>
		</rect>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Animated() {
		t.Fatal("animateTransform was not recognised")
	}
	flat, err := s.RenderAt(0xFFFFFFFF, 0)
	if err != nil {
		t.Fatal(err)
	}
	turned, err := s.RenderAt(0xFFFFFFFF, 250*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if string(flat.Pix) == string(turned.Pix) {
		t.Fatal("a quarter turn drew the same picture as the start")
	}
}

// TestStillSVGStaysStill keeps a plain file on the fast path.
func TestStillSVGStaysStill(t *testing.T) {
	s, err := Parse([]byte(testSVG))
	if err != nil {
		t.Fatal(err)
	}
	if s.Animated() || s.Duration() != 0 {
		t.Fatalf("a plain SVG reported animation: %v %v", s.Animated(), s.Duration())
	}
	if string(s.renderAt(time.Second, true)) != testSVG {
		t.Fatal("a still file was rewritten")
	}
}
