package text

import (
	"image"
	"math"
)

// blurGray returns src's coverage softened by a Gaussian whose radius is blur
// pixels, and how far the bitmap grew on each side. The caller moves the glyph's
// offset by that padding so the soft edge stays outside the tight glyph box.
//
// The kernel is separable, so the cost is two passes of 2r+1 taps instead of a
// square patch. Results are cached in the atlas per (glyph, blur), so a frame
// only pays this once per distinct glyph.
func blurGray(src image.Gray, blur int) (image.Gray, int) {
	if blur <= 0 {
		return src, 0
	}
	// CSS's blur radius is two standard deviations, and a Gaussian is visually
	// done at three, so the bitmap grows by 1.5·blur on each side.
	sigma := float64(blur) / 2
	radius := blur + blur/2 + 1
	kernel := gaussKernel(sigma, radius)

	w, h := src.Rect.Dx(), src.Rect.Dy()
	ew, eh := w+2*radius, h+2*radius

	// Copy the coverage into the grown buffer, then blur horizontally and
	// vertically. Floats keep the two passes from banding a large blur.
	row := make([]float32, ew)
	acc := make([]float32, ew*eh)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			acc[(y+radius)*ew+x+radius] = float32(src.Pix[y*src.Stride+x]) / 255
		}
	}
	for y := 0; y < eh; y++ {
		base := y * ew
		for x := 0; x < ew; x++ {
			var sum float32
			for k := -radius; k <= radius; k++ {
				x2 := x + k
				if x2 < 0 || x2 >= ew {
					continue
				}
				sum += acc[base+x2] * kernel[k+radius]
			}
			row[x] = sum
		}
		copy(acc[base:base+ew], row)
	}

	out := image.NewGray(image.Rect(0, 0, ew, eh))
	for x := 0; x < ew; x++ {
		for y := 0; y < eh; y++ {
			var sum float32
			for k := -radius; k <= radius; k++ {
				y2 := y + k
				if y2 < 0 || y2 >= eh {
					continue
				}
				sum += acc[y2*ew+x] * kernel[k+radius]
			}
			v := sum*255 + 0.5
			if v > 255 {
				v = 255
			}
			out.Pix[y*out.Stride+x] = uint8(v)
		}
	}
	return *out, radius
}

// gaussKernel builds a normalized one-dimensional Gaussian of the given radius.
func gaussKernel(sigma float64, radius int) []float32 {
	k := make([]float32, 2*radius+1)
	var sum float64
	for i := -radius; i <= radius; i++ {
		v := math.Exp(-float64(i*i) / (2 * sigma * sigma))
		k[i+radius] = float32(v)
		sum += v
	}
	for i := range k {
		k[i] /= float32(sum)
	}
	return k
}
