// Package text shapes, measures and rasterizes text. It owns the glyph atlas
// and the font faces, and is the only place that knows about HarfBuzz-quality
// shaping (through go-text/typesetting).
//
// Rasterization is a small scanline fill of the glyph outline with 4x vertical
// supersampling, cached in an 8-bit coverage atlas. That is enough for UI text
// at 1x and 2x; SDF would only help for heavy scaling.
package text

import (
	"bytes"
	"fmt"
	"image"
	"sync"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// Face is a font at one pixel size, ready to shape and rasterize.
type Face struct {
	font *font.Face
	tag  uint8 // font identity, so atlas keys never collide across fonts
	size float64
	upem float32
	// shaper is per-Face so the harfbuzz buffer is reused; shaping is not
	// goroutine-safe, and all text work happens on the UI goroutine.
	shaper shaping.HarfbuzzShaper
}

// Library holds the registered fonts and the glyph atlas. Create one per app.
type Library struct {
	mu    sync.Mutex
	fonts map[Weight]*font.Face
	icon  *font.Face
	faces map[faceKey]*Face
	atlas *Atlas
}

type faceKey struct {
	weight Weight
	size   int // 1/4 pixels, so 12.5 and 12.75 do not collide
	icon   bool
}

// NewLibrary returns a library with Roboto and Material Symbols loaded.
func NewLibrary() *Library {
	l := &Library{
		fonts: map[Weight]*font.Face{},
		faces: map[faceKey]*Face{},
		atlas: NewAtlas(2048, 2048),
	}
	load := func(w Weight, data []byte) {
		if len(data) == 0 {
			return
		}
		if f, err := font.ParseTTF(bytes.NewReader(data)); err == nil {
			l.fonts[w] = f
		}
	}
	load(WeightRegular, robotoRegular)
	load(WeightMedium, robotoMedium)
	load(WeightBold, robotoBold)
	if f, err := font.ParseTTF(bytes.NewReader(materialSymbols)); err == nil {
		l.icon = f
	}
	return l
}

// HasFont reports whether Roboto parsed.
func (l *Library) HasFont() bool { return l.fonts[WeightRegular] != nil }

// HasIcons reports whether Material Symbols parsed.
func (l *Library) HasIcons() bool { return l.icon != nil }

// RegisterFont adds a font at a weight, replacing the embedded one.
func (l *Library) RegisterFont(weight Weight, data []byte) error {
	f, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("text: parse font: %w", err)
	}
	l.mu.Lock()
	l.fonts[weight] = f
	l.mu.Unlock()
	return nil
}

// Atlas returns the glyph atlas, for the renderer to upload.
func (l *Library) Atlas() *Atlas { return l.atlas }

// Face returns the face for a size and weight, creating it once.
func (l *Library) Face(size float64, weight Weight) *Face {
	return l.face(size, weight, false)
}

// IconFace returns the face for Material Symbols at a size.
func (l *Library) IconFace(size float64) *Face {
	return l.face(size, WeightRegular, true)
}

func (l *Library) face(size float64, weight Weight, icon bool) *Face {
	if size <= 0 {
		size = 16
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	var src *font.Face
	var tag uint8
	if icon {
		src = l.icon
		tag = 3
	} else {
		src = l.fonts[weight]
		tag = 0
		switch weight {
		case WeightMedium:
			tag = 1
		case WeightBold:
			tag = 2
		}
		if src == nil {
			src = l.fonts[WeightRegular]
			tag = 0
		}
	}
	if src == nil {
		return nil
	}
	key := faceKey{weight: weight, size: int(size * 4), icon: icon}
	if f, ok := l.faces[key]; ok {
		return f
	}
	f := &Face{font: src, tag: tag, size: size, upem: float32(src.Upem())}
	l.faces[key] = f
	return f
}

// Tag returns the font identity used in atlas keys.
func (f *Face) Tag() uint8 { return f.tag }

// Size returns the pixel size of the face.
func (f *Face) Size() float64 { return f.size }

// Shape returns the laid-out glyphs for one line of text.
func (f *Face) Shape(s string) shaping.Output {
	runes := []rune(s)
	in := shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: di.DirectionLTR,
		Face:      f.font,
		Size:      fixed.Int26_6(f.size * 64),
		Script:    language.Latin,
		Language:  language.NewLanguage("en"),
	}
	return f.shaper.Shape(in)
}

// Measure returns the size of s at this face's size, wrapping at maxWidth when
// it is positive.
func (f *Face) Measure(s string, maxWidth float64) (w, h float64) {
	if s == "" {
		return 0, f.LineHeight()
	}
	out := f.Shape(s)
	adv := float64(out.Advance) / 64
	if maxWidth <= 0 || adv <= maxWidth {
		return adv, f.LineHeight()
	}
	// shortcut: line count is total advance divided by the wrap width, which is
	// right for Latin prose and wrong when one word is wider than the column.
	// Upgrade to real line breaking when Text gains multi-line rendering.
	lines := int(adv/maxWidth) + 1
	return maxWidth, float64(lines) * f.LineHeight()
}

// LineHeight returns the distance between baselines.
func (f *Face) LineHeight() float64 { return f.size * 1.2 }

// Ascent returns the distance from the baseline to the top of the line.
func (f *Face) Ascent() float64 { return f.size * 0.95 }

// GlyphID returns the nominal glyph for a rune.
func (f *Face) GlyphID(r rune) (uint16, bool) {
	g, ok := f.font.NominalGlyph(r)
	return uint16(g), ok
}

// Raster returns the coverage bitmap for one glyph at this size, plus its
// placement relative to the pen.
func (f *Face) Raster(gid uint16) (image.Gray, image.Point, bool) {
	if f == nil {
		return image.Gray{}, image.Point{}, false
	}
	data := f.font.GlyphData(font.GID(gid))
	outline, ok := data.(font.GlyphOutline)
	if !ok || len(outline.Segments) == 0 {
		return image.Gray{}, image.Point{}, false
	}
	scale := f.size / float64(f.upem)
	return rasterOutline(outline.Segments, scale)
}
