package text

import (
	"image"
	"math"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
)

// subSamples is the vertical supersampling factor used when filling a glyph.
const subSamples = 4

// italicSlant is the tangent of the synthetic italic angle (about 12°).
const italicSlant = 0.21

// rasterOutline fills the outline into a coverage bitmap. scale converts font
// units to pixels. italic shears the outline, since Roboto italic is not
// embedded and the shaper would otherwise draw an upright face. The returned
// point is the offset from the pen position to the bitmap's top-left corner, in
// whole pixels, with y growing down.
func rasterOutline(segs []font.Segment, scale float64, italic bool) (image.Gray, image.Point, bool) {
	contours, ok := flatten(segs)
	if !ok {
		return image.Gray{}, image.Point{}, false
	}
	if italic {
		// Font y grows up, so leaning right means adding to x with height.
		for i := range contours {
			for j := range contours[i] {
				contours[i][j][0] += italicSlant * contours[i][j][1]
			}
		}
	}

	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range contours {
		for _, p := range c {
			minX = math.Min(minX, float64(p[0]))
			maxX = math.Max(maxX, float64(p[0]))
			minY = math.Min(minY, float64(p[1]))
			maxY = math.Max(maxY, float64(p[1]))
		}
	}
	if math.IsInf(minX, 0) {
		return image.Gray{}, image.Point{}, false
	}

	// Device pixel bounds. Font y grows up, bitmap y grows down.
	x0 := int(math.Floor(minX * scale))
	x1 := int(math.Ceil(maxX * scale))
	top := -maxY * scale // smallest bitmap y
	bottom := -minY * scale
	y0 := int(math.Floor(top))
	y1 := int(math.Ceil(bottom))
	w, h := x1-x0, y1-y0
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 {
		return image.Gray{}, image.Point{}, false
	}

	img := image.NewGray(image.Rect(0, 0, w, h))
	acc := make([]float64, w)
	for py := 0; py < h; py++ {
		for s := 0; s < subSamples; s++ {
			// Sample in the middle of the sub-row, then map to font units.
			vy := float64(y0) + (float64(py) + (float64(s)+0.5)/float64(subSamples))
			fy := float32(-vy / scale)
			crossings := scanline(contours, fy)
			if len(crossings) == 0 {
				continue
			}
			fillRow(crossings, acc, x0, scale)
		}
		row := img.Pix[py*img.Stride : py*img.Stride+w]
		for x, v := range acc {
			// acc holds the sum over sub-rows, so average it back to 0..1.
			v /= float64(subSamples)
			if v > 1 {
				v = 1
			}
			row[x] = uint8(v*255 + 0.5)
			acc[x] = 0
		}
	}
	return *img, image.Point{X: x0, Y: y0}, true
}

// flatten converts outline segments into closed contours of line points.
func flatten(segs []font.Segment) ([][][2]float32, bool) {
	var contours [][][2]float32
	var cur [][2]float32
	var cx, cy float32
	push := func(x, y float32) { cur = append(cur, [2]float32{x, y}) }

	for _, s := range segs {
		args := s.ArgsSlice()
		switch s.Op {
		case opentype.SegmentOpMoveTo:
			if len(cur) > 2 {
				contours = append(contours, cur)
			}
			cur = nil
			cx, cy = args[0].X, args[0].Y
			push(cx, cy)
		case opentype.SegmentOpLineTo:
			cx, cy = args[0].X, args[0].Y
			push(cx, cy)
		case opentype.SegmentOpQuadTo:
			c, p := args[0], args[1]
			for i := 1; i <= 8; i++ {
				t := float32(i) / 8
				push(quadAt(cx, c.X, p.X, t), quadAt(cy, c.Y, p.Y, t))
			}
			cx, cy = p.X, p.Y
		case opentype.SegmentOpCubeTo:
			c1, c2, p := args[0], args[1], args[2]
			for i := 1; i <= 8; i++ {
				t := float32(i) / 8
				push(cubeAt(cx, c1.X, c2.X, p.X, t), cubeAt(cy, c1.Y, c2.Y, p.Y, t))
			}
			cx, cy = p.X, p.Y
		}
	}
	if len(cur) > 2 {
		contours = append(contours, cur)
	}
	return contours, len(contours) > 0
}

type crossing struct {
	x   float32
	dir int // +1 up, -1 down
}

// scanline returns the crossings at y, sorted ascending by x. fillRow then
// applies the nonzero winding rule.
//
// TrueType outlines may overlap themselves: the stem and the crossbar of a "t"
// are separate contours that cross, and the bowl of an "R" meets its stem. What
// matters there is the winding direction, not the parity — an even-odd fill
// treats the overlap as a hole and the glyph gets white gaps exactly where two
// strokes meet.
func scanline(contours [][][2]float32, y float32) []crossing {
	var xs []crossing
	for _, c := range contours {
		n := len(c)
		for i := 0; i < n; i++ {
			a := c[i]
			b := c[(i+1)%n]
			if a[1] == b[1] {
				continue
			}
			if (a[1] <= y) == (b[1] <= y) {
				continue
			}
			t := (y - a[1]) / (b[1] - a[1])
			dir := 1
			if a[1] > b[1] {
				dir = -1
			}
			xs = append(xs, crossing{x: a[0] + t*(b[0]-a[0]), dir: dir})
		}
	}
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j].x < xs[j-1].x; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
	return xs
}

// fillRow adds horizontal coverage between the spans enclosed by the outline
// into acc, which is indexed from the bitmap's left edge. A span is covered
// wherever the winding number is nonzero, so overlapping contours fill instead
// of cancelling out.
func fillRow(crossings []crossing, acc []float64, x0 int, scale float64) {
	winding := 0
	start := 0.0
	for _, c := range crossings {
		prev := winding
		winding += c.dir
		switch {
		case prev == 0 && winding != 0:
			start = float64(c.x)
		case prev != 0 && winding == 0:
			fillSpan(start, float64(c.x), acc, x0, scale)
		}
	}
}

// fillSpan adds the coverage of one horizontal run to acc.
func fillSpan(x1, x2 float64, acc []float64, x0 int, scale float64) {
	x1 = x1*scale - float64(x0)
	x2 = x2*scale - float64(x0)
	if x2 <= x1 {
		return
	}
	ix1 := int(math.Floor(x1))
	ix2 := int(math.Ceil(x2))
	for x := ix1; x < ix2; x++ {
		if x < 0 || x >= len(acc) {
			continue
		}
		lo := math.Max(x1, float64(x))
		hi := math.Min(x2, float64(x+1))
		if hi > lo {
			acc[x] += hi - lo
		}
	}
}

func quadAt(p0, p1, p2, t float32) float32 {
	u := 1 - t
	return u*u*p0 + 2*u*t*p1 + t*t*p2
}

func cubeAt(p0, p1, p2, p3, t float32) float32 {
	u := 1 - t
	return u*u*u*p0 + 3*u*u*t*p1 + 3*u*t*t*p2 + t*t*t*p3
}
