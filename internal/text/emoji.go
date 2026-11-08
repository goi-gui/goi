package text

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"

	"github.com/go-text/typesetting/font"
)

// Emoji is Noto Color Emoji, the font Android draws emoji with: the full set,
// including the newer Unicode releases, as colour bitmaps (CBDT). Embedding it
// costs the binary about 11 MB, which is the price of "every emoji, in colour,
// offline"; a build that does not want it can drop the file and the node falls
// back to the text face's monochrome glyphs.
//
//go:embed fonts/noto-color-emoji.ttf
var notoEmoji []byte

// HasEmoji reports whether the colour emoji font parsed.
func (l *Library) HasEmoji() bool { return l.emoji != nil }

// EmojiFace returns the colour emoji face at a size. Text never shapes with it
// directly: the text face falls back to it for a rune Roboto has no glyph for.
func (l *Library) EmojiFace(size float64) *Face {
	if l.emoji == nil {
		return nil
	}
	if size <= 0 {
		size = 16
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := faceKey{emoji: true, size: int(size * 4)}
	if f, ok := l.faces[key]; ok {
		return f
	}
	f := &Face{font: l.emoji, tag: 16, size: size, upem: float32(l.emoji.Upem()), emoji: true, lib: l}
	l.faces[key] = f
	return f
}

// IsEmoji reports whether the face draws colour emoji bitmaps.
func (f *Face) IsEmoji() bool { return f != nil && f.emoji }

// covers reports whether the face has a glyph for r, which is what decides the
// fallback boundary.
func (f *Face) covers(r rune) bool {
	if f == nil || f.font == nil {
		return false
	}
	_, ok := f.font.NominalGlyph(r)
	return ok
}

// emojiRune reports whether r is drawn by the emoji font: an emoji or a
// pictograph Roboto has no glyph for. A variation selector or a zero-width
// joiner stays with the run it belongs to, so a sequence such as a family emoji
// is shaped as one run.
func emojiRune(f *Face, r rune) bool {
	if f.lib == nil || f.lib.emoji == nil || f.emoji {
		return false
	}
	if r == 0x200D || r == 0xFE0F || r == 0xFE0E {
		return true
	}
	if f.covers(r) {
		return false
	}
	return r >= 0x203C && f.lib.emojiCovers(r)
}

// emojiCovers reports whether the emoji font has a glyph for r.
func (l *Library) emojiCovers(r rune) bool {
	if l.emoji == nil {
		return false
	}
	_, ok := l.emoji.NominalGlyph(r)
	return ok
}

// emojiKey identifies one colour glyph at one pixel size.
type emojiKey struct {
	size int // 1/4 pixels
	gid  uint16
}

// emojiGlyph is a decoded colour glyph: its pixels and where its top-left sits
// relative to the pen, in whole pixels with y growing down.
type emojiGlyph struct {
	img  *image.RGBA
	off  image.Point
	size int
}

// ColorGlyph returns a colour glyph's image at this face's size. It is what a
// renderer draws instead of an outline, and reports false for a glyph that has
// no colour bitmap.
func (f *Face) ColorGlyph(gid uint16) (*image.RGBA, image.Point, bool) {
	if !f.IsEmoji() {
		return nil, image.Point{}, false
	}
	key := emojiKey{size: int(f.size * 4), gid: gid}
	f.lib.mu.Lock()
	if g, ok := f.lib.emojiCache[key]; ok {
		f.lib.mu.Unlock()
		return g.img, g.off, true
	}
	// GlyphDataBitmap picks a strike from the face's ppem, and every face of a
	// font shares one *font.Face, so the size is set right before the read.
	f.font.SetPpem(uint16(f.size), uint16(f.size))
	data := f.font.GlyphData(font.GID(gid))
	f.lib.mu.Unlock()
	bmp, ok := data.(font.GlyphBitmap)
	if !ok || bmp.Format != font.PNG || len(bmp.Data) == 0 {
		return nil, image.Point{}, false
	}
	src, err := png.Decode(bytes.NewReader(bmp.Data))
	if err != nil {
		return nil, image.Point{}, false
	}
	w, h := f.emojiPixels(bmp.Width, bmp.Height)
	scaled := scaleRGBA(src, w, h)

	// Place the bitmap on the baseline: the em box sits above it, and the
	// advance is the glyph's own, which the shaper already applied.
	// Sit the bitmap on the baseline with a tenth of the size below it, which is
	// where an emoji reads best next to a line of text.
	off := image.Point{X: 0, Y: int(f.size*0.1) - scaled.Rect.Dy()}
	g := &emojiGlyph{img: scaled, off: off, size: key.size}
	f.lib.mu.Lock()
	if f.lib.emojiCache == nil || len(f.lib.emojiCache) > 1024 {
		f.lib.emojiCache = map[emojiKey]*emojiGlyph{}
	}
	f.lib.emojiCache[key] = g
	f.lib.mu.Unlock()
	return g.img, g.off, true
}

// emojiScale is the ratio between a strike's pixels and the face's size, so an
// emoji is drawn at the size of the text around it whatever strike the font
// ships.
func (f *Face) emojiScale(strikeW int) float64 {
	if strikeW <= 0 {
		return 1
	}
	return f.size / float64(strikeW)
}

// emojiPixels is the size a colour glyph should be drawn at: the face's size,
// keeping the strike's aspect.
func (f *Face) emojiPixels(w, h int) (int, int) {
	scale := f.emojiScale(w)
	return max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
}

// scaleRGBA is a box filter to the given pixel size. A strike is fixed, so a
// 16px line downsamples it; the filter keeps the emoji's edges clean.
func scaleRGBA(src image.Image, w, h int) *image.RGBA {
	bd := src.Bounds()
	if bd.Dx() == w && bd.Dy() == h {
		if r, ok := src.(*image.RGBA); ok {
			return r
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	sx, sy := float64(bd.Dx())/float64(w), float64(bd.Dy())/float64(h)
	for y := 0; y < h; y++ {
		y0, y1 := bd.Min.Y+int(float64(y)*sy), bd.Min.Y+int(float64(y+1)*sy)
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := bd.Min.X+int(float64(x)*sx), bd.Min.X+int(float64(x+1)*sx)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, a uint32
			var n uint32
			for yy := y0; yy < y1 && yy < bd.Max.Y; yy++ {
				for xx := x0; xx < x1 && xx < bd.Max.X; xx++ {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r += cr
					g += cg
					b += cb
					a += ca
					n++
				}
			}
			if n == 0 {
				continue
			}
			i := out.PixOffset(x, y)
			out.Pix[i] = uint8(r / n >> 8)
			out.Pix[i+1] = uint8(g / n >> 8)
			out.Pix[i+2] = uint8(b / n >> 8)
			out.Pix[i+3] = uint8(a / n >> 8)
		}
	}
	return out
}

// loadEmoji parses the embedded colour font; a failure is not fatal, the text
// face's monochrome glyphs are still there.
func (l *Library) loadEmoji(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	f, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("text: parse emoji font: %w", err)
	}
	l.emoji = f
	return nil
}
