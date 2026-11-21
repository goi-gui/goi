//go:build all || svg

package svg

import (
	"bytes"
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

// TestRoundedRectUsesRxForRy covers the SVG default oksvg misses: a rect with
// rx but no ry is rounded in both corners, so the logo background keeps its
// radius.
func TestRoundedRectUsesRxForRy(t *testing.T) {
	const rounded = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<rect width="100" height="100" rx="20" fill="currentColor"/>
	</svg>`
	s, err := Parse([]byte(rounded))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0x0088FFFF)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(d, 0, 0); a > 10 {
		t.Fatalf("corner alpha = %d, want transparent", a)
	}
	if _, _, _, a := pixelAt(d, d.W/2, d.H/2); a < 250 {
		t.Fatalf("centre alpha = %d, want opaque", a)
	}
	if _, _, _, a := pixelAt(d, d.W/2, 2); a < 250 {
		t.Fatalf("top edge alpha = %d, want filled between the corners", a)
	}
}

// TestStrokeWidthFollowsRasterScale: oksvg scales the path but not the stroke
// width, so a 2px line in a 10px viewBox must scale with the raster instead of
// staying a hairline.
func TestStrokeWidthFollowsRasterScale(t *testing.T) {
	const stroked = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
		<line x1="5" y1="1" x2="5" y2="9" stroke="currentColor" stroke-width="2"/>
	</svg>`
	s, err := Parse([]byte(stroked))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	row := d.H / 2
	opaque := 0
	for x := 0; x < d.W; x++ {
		if _, _, _, a := pixelAt(d, x, row); a > 40 {
			opaque++
		}
	}
	want := int(2 * float64(d.W) / 10)
	if opaque < want-2 || opaque > want+3 {
		t.Fatalf("stroke row = %d pixels, want about %d", opaque, want)
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
	if got := string(s.renderAt(time.Second, true)); bytes.Contains([]byte(got), []byte("<animate")) || !bytes.Contains([]byte(got), []byte("<rect")) {
		t.Fatalf("a still file gained an animation or lost its shape: %s", got)
	}
}

func TestInlineStyleGradientFollowsHref(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs>
			<linearGradient id="base">
				<stop offset="0" style="stop-color:#0000ff;stop-opacity:1"/>
				<stop offset="1" style="stop-color:#ff0000;stop-opacity:1"/>
			</linearGradient>
			<linearGradient id="g" href="#base" x1="0" y1="0" x2="1" y2="0"/>
		</defs>
		<rect width="100" height="100" style="fill:url(#g) rgb(0,0,0)"/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := pixelAt(d, 10, d.H/2)
	if a < 250 || b < 220 || r > 30 || g > 30 {
		t.Fatalf("left = %d,%d,%d,%d, want the blue stop", r, g, b, a)
	}
	r, g, b, a = pixelAt(d, d.W-10, d.H/2)
	if a < 250 || r < 220 || g > 30 || b > 30 {
		t.Fatalf("right = %d,%d,%d,%d, want the red stop", r, g, b, a)
	}
}

func TestSingleArgumentScaleDraws(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 108 95">
		<g transform="scale(0.1)"><path fill="red" d="M100,100 L900,100 L900,900 Z"/></g>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(d, d.W/2, d.H/2); a < 250 {
		t.Fatalf("single-argument scale drew nothing (alpha %d)", a)
	}
}

func TestWidthHeightGivesIntrinsicSize(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">
		<rect width="100" height="50" fill="red"/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := s.Intrinsic(); w != 100 || h != 50 {
		t.Fatalf("intrinsic = %vx%v, want 100x50", w, h)
	}
}

func TestNegativeViewBoxOrigin(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-50 -50 100 100">
		<rect x="-50" y="-50" width="100" height="100" fill="red"/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]int{{2, 2}, {d.W - 3, 2}, {2, d.H - 3}, {d.W - 3, d.H - 3}} {
		if _, _, _, a := pixelAt(d, p[0], p[1]); a < 250 {
			t.Fatalf("corner %v alpha = %d, want the shifted box filled", p, a)
		}
	}
}

func TestNestedSVGViewport(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200">
		<rect width="200" height="200" fill="white"/>
		<svg x="50" y="50" width="100" height="100" viewBox="0 0 10 10">
			<rect width="10" height="10" fill="red"/>
		</svg>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := pixelAt(d, d.W/2, d.H/2); a < 250 || r < 220 || g > 30 || b > 30 {
		t.Fatalf("centre = %d,%d,%d,%d, want red", r, g, b, a)
	}
	if r, g, b, a := pixelAt(d, 10, 10); a < 250 || r < 220 || g < 220 || b < 220 {
		t.Fatalf("corner = %d,%d,%d,%d, want white outside the nested viewport", r, g, b, a)
	}
}

func TestSymbolUseDraws(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs><symbol id="s" viewBox="0 0 10 10"><rect width="10" height="10" fill="red"/></symbol></defs>
		<use href="#s" x="10" y="10" width="40" height="40"/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(d, d.W/4, d.H/4); a < 250 {
		t.Fatalf("symbol use drew nothing (alpha %d)", a)
	}
}

// TestSVGStyleClassesResolve keeps an exported icon working: its colours live in
// a stylesheet (`class` + `var(--indicator)`), which oksvg ignores unless the
// rule is folded into an attribute.
func TestSVGStyleClassesResolve(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
		<style>
			:root { --ink: #00ff00; }
			.shape { fill: var(--ink); }
		</style>
		<rect width="10" height="10" class="shape"/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := pixelAt(d, d.W/2, d.H/2)
	if a < 250 || g < 240 || r > 15 || b > 15 {
		t.Fatalf("class colour = %d,%d,%d,%d, want the green from the stylesheet", r, g, b, a)
	}
}

// TestSVGPathMorphs checks a `d` animation: the shape's numbers interpolate and
// the result still rasterizes, which is what an exported loading icon needs.
func TestSVGPathMorphs(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
		<path fill="#000000" d="M1,1 L9,1 L9,9 L1,9 Z">
			<animate attributeName="d" dur="1s" repeatCount="indefinite"
				values="M1,1 L9,1 L9,9 L1,9 Z;M2,2 L8,2 L8,8 L2,8 Z"/>
		</path>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	start, err := s.RenderAt(0xFFFFFFFF, 0)
	if err != nil {
		t.Fatal(err)
	}
	mid, err := s.RenderAt(0xFFFFFFFF, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("a morphed path failed to rasterize: %v", err)
	}
	if string(start.Pix) == string(mid.Pix) {
		t.Fatal("the morphed path drew the same pixels as the start")
	}
	// Halfway between 1 and 2 is 1.5, so the top-left pixel stays empty.
	if _, _, _, a := pixelAt(mid, int(float64(mid.W)*0.05), int(float64(mid.H)*0.05)); a > 40 {
		t.Fatalf("the morphed shape still covers the corner (alpha %d)", a)
	}
}

// TestBlendPathKeepsLetters pins the interpolation itself.
func TestBlendPathKeepsLetters(t *testing.T) {
	got, ok := blendPath("M0,0 L10,10 Z", "M10,10 L20,20 Z", 0.5)
	if !ok {
		t.Fatal("same-shape paths did not blend")
	}
	want := "M 5 5L 15 15Z"
	if got != want {
		t.Fatalf("blend = %q, want %q", got, want)
	}
	if _, ok := blendPath("M0,0 L10,10 Z", "M0,0 C1,1 2,2 3,3 Z", 0.5); ok {
		t.Fatal("paths with different commands were blended")
	}
}

func TestRootClipPath(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs><clipPath id="c"><circle cx="50" cy="50" r="30"/></clipPath></defs>
		<rect width="100" height="100" fill="red" clip-path="url(#c)"/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := pixelAt(d, d.W/2, d.H/2); a < 250 || r < 220 || g > 30 || b > 30 {
		t.Fatalf("centre = %d,%d,%d,%d, want red", r, g, b, a)
	}
	if _, _, _, a := pixelAt(d, 2, 2); a > 10 {
		t.Fatalf("corner alpha = %d, want the circle clip", a)
	}
}

func TestGroupClipPath(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs><clipPath id="c"><circle cx="50" cy="50" r="30"/></clipPath></defs>
		<g clip-path="url(#c)"><rect width="100" height="100" fill="red"/></g>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(d, 2, 2); a > 10 {
		t.Fatalf("corner alpha = %d, want the group clipped", a)
	}
}

func TestRootMask(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs><mask id="m"><circle cx="50" cy="50" r="30" fill="white"/></mask></defs>
		<rect width="100" height="100" fill="red" mask="url(#m)"/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := pixelAt(d, d.W/2, d.H/2); a < 250 || r < 220 || g > 30 || b > 30 {
		t.Fatalf("centre = %d,%d,%d,%d, want the mask to keep red", r, g, b, a)
	}
	if _, _, _, a := pixelAt(d, 2, 2); a > 10 {
		t.Fatalf("corner alpha = %d, want the mask to hide it", a)
	}
}

func TestTextDraws(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50">
		<text x="10" y="30" font-size="20" fill="red">Hi</text>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	red := 0
	for i := 0; i < len(d.Pix); i += 4 {
		if d.Pix[i] > 200 && d.Pix[i+1] < 60 && d.Pix[i+3] > 0 {
			red++
		}
	}
	if red < 20 {
		t.Fatalf("red text pixels = %d, want a drawn label", red)
	}
}

func TestEmbeddedImage(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<image x="10" y="10" width="40" height="40" href="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAEUlEQVR4nGP4z8DwH4QZYAwAR8oH+WdZbrcAAAAASUVORK5CYII="/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := pixelAt(d, d.W/5, d.H/5); a < 250 || r < 220 || g > 30 || b > 30 {
		t.Fatalf("image centre = %d,%d,%d,%d, want red", r, g, b, a)
	}
	if _, _, _, a := pixelAt(d, 2, 2); a > 10 {
		t.Fatalf("outside image alpha = %d, want transparent", a)
	}
}

func TestRootGaussianBlur(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs><filter id="f"><feGaussianBlur stdDeviation="2"/></filter></defs>
		<g filter="url(#f)"><rect x="40" y="40" width="20" height="20" fill="red"/></g>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(d, 38*d.W/100, 50*d.H/100); a < 10 || a > 240 {
		t.Fatalf("blurred edge alpha = %d, want a soft falloff", a)
	}
}

func TestRootDropShadow(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs><filter id="f"><feDropShadow dx="4" dy="4" stdDeviation="1" flood-color="#000000" flood-opacity="0.8"/></filter></defs>
		<g filter="url(#f)"><rect x="30" y="30" width="30" height="30" fill="red"/></g>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(d, 63*d.W/100, 63*d.H/100); a < 20 {
		t.Fatalf("shadow alpha = %d, want a soft shadow below the box", a)
	}
	if r, g, b, a := pixelAt(d, 45*d.W/100, 45*d.H/100); a < 250 || r < 220 || g > 30 || b > 30 {
		t.Fatalf("box centre = %d,%d,%d,%d, want red", r, g, b, a)
	}
}

func TestPatternFill(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs><pattern id="p" width="10" height="10" patternUnits="userSpaceOnUse">
			<rect width="5" height="10" fill="red"/>
			<rect x="5" width="5" height="10" fill="blue"/>
		</pattern></defs>
		<rect width="100" height="100" fill="url(#p)"/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := pixelAt(d, 10*d.W/100, 50*d.H/100)
	if a < 250 || r < 220 || g > 30 || b > 30 {
		t.Fatalf("pattern red cell = %d,%d,%d,%d", r, g, b, a)
	}
	r, g, b, a = pixelAt(d, 15*d.W/100, 50*d.H/100)
	if a < 250 || b < 220 || r > 30 || g > 30 {
		t.Fatalf("pattern blue cell = %d,%d,%d,%d", r, g, b, a)
	}
}

func TestElementFilter(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs><filter id="f"><feGaussianBlur stdDeviation="2"/></filter></defs>
		<rect x="40" y="40" width="20" height="20" fill="red" filter="url(#f)"/>
	</svg>`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Render(0xFFFFFFFF)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := pixelAt(d, 38*d.W/100, 50*d.H/100); a < 10 || a > 240 {
		t.Fatalf("filtered edge alpha = %d, want a soft falloff", a)
	}
}
