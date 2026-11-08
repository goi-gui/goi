// Package img decodes image files into the one pixel format the renderer
// uploads, and packs several of them into a single GPU texture.
package img

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"  // register decoders for the file formats a design may use
	_ "image/jpeg" // register
	_ "image/png"  // register
)

// Data is a decoded image: tightly packed RGBA, straight (not premultiplied)
// alpha, ready to upload.
type Data struct {
	W, H int
	Pix  []byte
	// Gen counts in-place updates: an animation swaps Pix and bumps it, which
	// tells the renderer to upload this image's atlas region again instead of
	// packing a new one.
	Gen int
}

// Decode reads a PNG, JPEG or GIF. Anything else is an error naming the format,
// so a typo in a path does not look like a decode failure.
func Decode(b []byte) (*Data, error) {
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("img: decode: %w", err)
	}
	return FromImage(src), nil
}

// FromImage converts any decoded image into the renderer's pixel format, which
// is what Image.SetImage accepts from Go.
func FromImage(src image.Image) *Data {
	rgba, ok := src.(*image.RGBA)
	if !ok {
		bd := src.Bounds()
		rgba = image.NewRGBA(image.Rect(0, 0, bd.Dx(), bd.Dy()))
		draw.Draw(rgba, rgba.Bounds(), src, bd.Min, draw.Src)
	}
	return &Data{W: rgba.Rect.Dx(), H: rgba.Rect.Dy(), Pix: rgba.Pix}
}

// Shelf is a simple rectangle packer: images are placed in rows on shelves, and
// when one does not fit the shelf is closed and a new one starts. It is exact
// for the common case of a few images of similar height, and cheap.
type Shelf struct {
	W, H     int
	x, y     int
	shelfH   int
	occupied bool
}

// Place returns the top-left corner for a w by h image, or false when it does
// not fit in the remaining space.
func (s *Shelf) Place(w, h int) (x, y int, ok bool) {
	if w <= 0 || h <= 0 || w > s.W {
		return 0, 0, false
	}
	if s.x+w > s.W {
		if !s.occupied {
			return 0, 0, false
		}
		s.y += s.shelfH
		s.x, s.shelfH = 0, 0
	}
	if s.y+h > s.H {
		return 0, 0, false
	}
	x, y = s.x, s.y
	s.x += w
	if h > s.shelfH {
		s.shelfH = h
	}
	s.occupied = true
	return x, y, true
}

// Region is a placement inside a packed texture.
type Region struct{ X, Y, W, H int }

// Scale returns a box-filtered copy of d at w by h. Nearest would look rough for
// a photo, and this only runs when an image is too large for the atlas.
func Scale(d *Data, w, h int) *Data {
	out := &Data{W: w, H: h, Pix: make([]byte, w*h*4)}
	sx, sy := float64(d.W)/float64(w), float64(d.H)/float64(h)
	for y := 0; y < h; y++ {
		y0, y1 := int(float64(y)*sy), int(float64(y+1)*sy)
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := int(float64(x)*sx), int(float64(x+1)*sx)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, a uint32
			var n uint32
			for yy := y0; yy < y1 && yy < d.H; yy++ {
				for xx := x0; xx < x1 && xx < d.W; xx++ {
					i := (yy*d.W + xx) * 4
					r += uint32(d.Pix[i])
					g += uint32(d.Pix[i+1])
					b += uint32(d.Pix[i+2])
					a += uint32(d.Pix[i+3])
					n++
				}
			}
			if n == 0 {
				continue
			}
			i := (y*w + x) * 4
			out.Pix[i] = uint8(r / n)
			out.Pix[i+1] = uint8(g / n)
			out.Pix[i+2] = uint8(b / n)
			out.Pix[i+3] = uint8(a / n)
		}
	}
	return out
}
