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
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

// Face is a font at one pixel size, ready to shape and rasterize.
type Face struct {
	font *font.Face
	size float64
	upem float32

	// shaper is per-Face so the harfbuzz buffer is reused; shaping is not
	// goroutine-safe, and all text work happens on the UI goroutine.
	shaper shaping.HarfbuzzShaper
}

// Library holds the registered fonts and the glyph atlas. Create one per app.
type Library struct {
	mu     sync.Mutex
	fonts  []*font.Face
	faces  map[faceKey]*Face
	atlas  *Atlas
	loaded bool
}

type faceKey struct {
	font int
	size int // size in 1/4 pixels, so 12.5 and 12.75 do not collide
}

// NewLibrary returns a library with the embedded default font registered.
func NewLibrary() *Library {
	l := &Library{faces: map[faceKey]*Face{}, atlas: NewAtlas(1024, 1024)}
	if f, err := font.ParseTTF(bytes.NewReader(goregular.TTF)); err == nil {
		l.fonts = append(l.fonts, f)
		l.loaded = true
	}
	return l
}

// HasFont reports whether the default font parsed.
func (l *Library) HasFont() bool { return l.loaded }

// RegisterFont adds a font for fallback use. It is appended, so the default
// font keeps priority for glyphs it has.
func (l *Library) RegisterFont(ttf []byte) error {
	f, err := font.ParseTTF(bytes.NewReader(ttf))
	if err != nil {
		return fmt.Errorf("text: parse font: %w", err)
	}
	l.mu.Lock()
	l.fonts = append(l.fonts, f)
	l.loaded = true
	l.mu.Unlock()
	return nil
}

// Atlas returns the glyph atlas, for the renderer to upload.
func (l *Library) Atlas() *Atlas { return l.atlas }

// Face returns the face for a size, parsing once per size.
func (l *Library) Face(size float64) *Face {
	if size <= 0 {
		size = 16
	}
	if !l.loaded {
		return nil
	}
	key := faceKey{font: 0, size: int(size * 4)}
	l.mu.Lock()
	defer l.mu.Unlock()
	if f, ok := l.faces[key]; ok {
		return f
	}
	f := &Face{
		font: l.fonts[0],
		size: size,
		upem: float32(l.fonts[0].Upem()),
	}
	l.faces[key] = f
	return f
}

// Shape returns the laid-out glyphs for one line of text.
func (f *Face) Shape(s string) shaping.Output {
	in := shaping.Input{
		Text:      []rune(s),
		RunStart:  0,
		RunEnd:    len([]rune(s)),
		Direction: di.DirectionLTR,
		Face:      f.fontFace(),
		Size:      fixed.Int26_6(f.size * 64),
		Script:    language.Latin,
		Language:  language.NewLanguage("en"),
	}
	return f.shaper.Shape(in)
}

// fontFace returns the shaping face for the current size.
func (f *Face) fontFace() *font.Face {
	return f.font
}

// Measure returns the size of s at this face's size, wrapping at maxWidth when
// it is positive. The height is one line unless wrapping adds more.
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
	// Upgrade to real line breaking (go-text/typesetting/segmenter) when Text
	// gains multi-line rendering.
	lines := int(adv/maxWidth) + 1
	return maxWidth, float64(lines) * f.LineHeight()
}

// LineHeight returns the distance between baselines.
func (f *Face) LineHeight() float64 { return f.size * 1.2 }

// Ascent returns the distance from the baseline to the top of the line.
func (f *Face) Ascent() float64 { return f.size * 0.95 }

// Raster returns the cached coverage bitmap for one glyph at this size, plus
// its integer placement relative to the pen.
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
