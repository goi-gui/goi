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
	font   *font.Face
	tag    uint8 // font identity, so atlas keys never collide across fonts
	size   float64
	upem   float32
	italic bool
	// shaper is per-Face so the harfbuzz buffer is reused; shaping is not
	// goroutine-safe, and all text work happens on the UI goroutine.
	shaper shaping.HarfbuzzShaper
	// adv caches the advance of a run. Layout wraps text by measuring growing
	// prefixes, so without this every frame re-shapes the same strings and a
	// full relayout costs milliseconds. Cleared when it grows past a bound.
	adv map[string]float64
	// shaped caches whole shaping results, glyphs included. Every repaint runs
	// through Shape for each text run, and shaping a short line costs ~20us, so
	// an unchanged page would otherwise pay milliseconds per frame just to
	// re-shape the same strings. The results are read-only to every caller.
	shaped map[string]shaping.Output
	// emoji marks a colour-emoji face, whose glyphs are bitmaps rather than
	// outlines; lib is the library that owns the fallback chain.
	emoji bool
	lib   *Library
}

// maxShapedRuns bounds the shaping cache. A page has far fewer distinct runs
// than this; a design that exceeds it drops the whole map rather than growing
// without limit, and the runs it still shows are re-shaped once.
const maxShapedRuns = 512

// Library holds the registered fonts and the glyph atlas. Create one per app.
type Library struct {
	mu    sync.Mutex
	fonts map[Weight]*font.Face
	icon  *font.Face
	// emoji is Noto Color Emoji, the face a rune Roboto has no glyph for falls
	// back to; emojiCache holds its decoded bitmaps per size and glyph.
	emoji      *font.Face
	emojiCache map[emojiKey]*emojiGlyph
	faces      map[faceKey]*Face
	atlas      *Atlas
	// front is a small direct-mapped cache in front of faces. A page uses a
	// handful of sizes and asks for each of them hundreds of times per pass, and
	// it alternates between them (12, 14, 16, 22), so remembering only the last
	// one missed most of the time. Four slots keyed by the size absorb that
	// pattern without a map lookup or a hash.
	front [4]faceSlot
}

type faceKey struct {
	weight Weight
	size   int // 1/4 pixels, so 12.5 and 12.75 do not collide
	icon   bool
	italic bool
	emoji  bool
}

// faceSlot is one entry of the direct-mapped face cache.
type faceSlot struct {
	key faceKey
	f   *Face
}

// NewLibrary returns a library with Roboto and Material Symbols loaded.
func NewLibrary() *Library {
	l := &Library{
		fonts: map[Weight]*font.Face{},
		faces: map[faceKey]*Face{},
		// A small window fits in 1024² with room to spare, and the atlas grows
		// when a page needs more. Starting at the maximum spent 4MB on every
		// app, most of it never touched.
		atlas: NewAtlas(1024, 1024),
	}
	load := func(w Weight, data []byte) {
		if len(data) == 0 {
			return
		}
		if f, err := font.ParseTTF(bytes.NewReader(data)); err == nil {
			l.fonts[w] = f
		}
	}
	load(WeightLight, robotoLight)
	load(WeightRegular, robotoRegular)
	load(WeightMedium, robotoMedium)
	load(WeightBold, robotoBold)
	load(WeightBlack, robotoBlack)
	if f, err := font.ParseTTF(bytes.NewReader(materialSymbols)); err == nil {
		l.icon = f
	}
	l.loadEmoji(notoEmoji)
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
	return l.face(size, weight, false, false)
}

// ItalicFace returns the same face slanted for `fontStyle: italic`. Roboto has
// no italic file embedded, so the slant is synthesized at raster time.
func (l *Library) ItalicFace(size float64, weight Weight) *Face {
	return l.face(size, weight, false, true)
}

// IconFace returns the face for Material Symbols at a size.
func (l *Library) IconFace(size float64) *Face {
	return l.face(size, WeightRegular, true, false)
}

func (l *Library) face(size float64, weight Weight, icon, italic bool) *Face {
	if size <= 0 {
		size = 16
	}
	if icon {
		italic = false
	} else {
		weight = NearestWeight(weight)
	}
	key := faceKey{weight: weight, size: int(size * 4), icon: icon, italic: italic}
	l.mu.Lock()
	defer l.mu.Unlock()
	slot := &l.front[(key.size^int(weight)*7^boolInt(italic)*5)&3]
	if slot.f != nil && slot.key == key {
		return slot.f
	}

	var src *font.Face
	var tag uint8
	if icon {
		src = l.icon
		tag = 3
	} else {
		src = l.fonts[weight]
		tag = 0
		switch weight {
		case WeightLight:
			tag = 4
		case WeightMedium:
			tag = 1
		case WeightBold:
			tag = 2
		case WeightBlack:
			tag = 5
		}
		if src == nil {
			src = l.fonts[WeightRegular]
			tag = 0
		}
		if italic {
			// Keep italic glyphs in their own atlas entries.
			tag += 8
		}
	}
	if src == nil {
		return nil
	}
	if f, ok := l.faces[key]; ok {
		*slot = faceSlot{key: key, f: f}
		return f
	}
	f := &Face{font: src, tag: tag, size: size, upem: float32(src.Upem()), italic: italic, lib: l}
	l.faces[key] = f
	*slot = faceSlot{key: key, f: f}
	return f
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Tag returns the font identity used in atlas keys.
func (f *Face) Tag() uint8 { return f.tag }

// Size returns the pixel size of the face.
func (f *Face) Size() float64 { return f.size }

// Shape returns the laid-out glyphs for one line of text, in visual order.
//
// The line is split into bidirectional runs, each shaped with its own direction
// and script, then the runs are ordered for display. A right-to-left line is
// shaped as one run, which is the common case (Arabic, Hebrew) and fully
// correct; a mixed line keeps each run in the right internal order and places
// them with the paragraph's base direction.
//
// shortcut: caret mapping in a field still measures text prefixes with a left
// to right assumption, so a caret inside right-to-left text lands between
// clusters rather than on them. Upgrade with a real cluster-to-glyph map when a
// field needs to edit mixed scripts.
func (f *Face) Shape(s string) shaping.Output {
	if out, ok := f.shaped[s]; ok {
		return out
	}
	out := f.shape(s)
	if f.shaped == nil || len(f.shaped) >= maxShapedRuns {
		f.shaped = make(map[string]shaping.Output)
	}
	f.shaped[s] = out
	return out
}

// ShapedRun is one stretch of a line with the face that shaped it. A run is
// split at a font boundary, so a line with an emoji in it yields a Roboto run
// and an emoji run, and a renderer knows which glyphs to draw as bitmaps.
type ShapedRun struct {
	Face   *Face
	Output shaping.Output
}

// ShapeRuns shapes s and reports which face drew each run: emoji that Roboto
// has no glyph for fall back to the colour font. Widths are the same as Shape
// measures, so layout and paint always agree.
func (f *Face) ShapeRuns(s string) []ShapedRun {
	var out []ShapedRun
	for _, seg := range f.splitFont(s) {
		face := f
		if seg.emoji {
			face = f.lib.EmojiFace(f.size)
			if face == nil {
				face = f
			}
		}
		out = append(out, ShapedRun{Face: face, Output: face.shapeBidi(seg.text)})
	}
	return out
}

// splitFont cuts a line where the face changes: ordinary text stays with this
// face, and a rune it does not cover goes to the emoji face.
func (f *Face) splitFont(s string) []fontSeg {
	if f.lib == nil || f.lib.emoji == nil || f.emoji {
		return []fontSeg{{text: s}}
	}
	var out []fontSeg
	start := 0
	emoji := false
	for i, r := range s {
		e := emojiRune(f, r)
		if i == 0 {
			emoji = e
			continue
		}
		if e == emoji {
			continue
		}
		out = append(out, fontSeg{text: s[start:i], emoji: emoji})
		start, emoji = i, e
	}
	out = append(out, fontSeg{text: s[start:], emoji: emoji})
	return out
}

// fontSeg is one stretch of a line with one face.
type fontSeg struct {
	text  string
	emoji bool
}

// shapeBidi shapes one face's stretch, honouring its runs' directions.
func (f *Face) shapeBidi(s string) shaping.Output {
	runs := splitBidi(s)
	if len(runs) == 1 && !runs[0].rtl {
		return f.shapeRun(s, di.DirectionLTR, runs[0].script)
	}
	out := shaping.Output{Size: fixed.Int26_6(f.size * 64)}
	chunks := make([][]shaping.Glyph, 0, len(runs))
	for _, r := range runs {
		dir := di.DirectionLTR
		if r.rtl {
			dir = di.DirectionRTL
		}
		sub := f.shapeRun(r.text, dir, r.script)
		chunks = append(chunks, sub.Glyphs)
		out.Advance += sub.Advance
	}
	// Runs are built in logical order. A right-to-left paragraph lays them out
	// from the right, so the run order flips; glyphs inside a run are already in
	// visual order from the shaper.
	if baseDirection(s) {
		for i, j := 0, len(chunks)-1; i < j; i, j = i+1, j-1 {
			chunks[i], chunks[j] = chunks[j], chunks[i]
		}
	}
	for _, c := range chunks {
		out.Glyphs = append(out.Glyphs, c...)
	}
	return out
}

// shape runs the shaper; Shape caches the result. Emoji fallback is folded in,
// so a width measured here matches what ShapeRuns reports for the same line.
func (f *Face) shape(s string) shaping.Output {
	if f.lib != nil && f.lib.emoji != nil && !f.emoji {
		runs := f.ShapeRuns(s)
		if len(runs) > 1 {
			out := shaping.Output{Size: fixed.Int26_6(f.size * 64)}
			for _, r := range runs {
				out.Advance += r.Output.Advance
				out.Glyphs = append(out.Glyphs, r.Output.Glyphs...)
			}
			return out
		}
	}
	return f.shapeBidi(s)
}

// bidiRun is one stretch of text with a single direction and script.
type bidiRun struct {
	text   string
	rtl    bool
	script language.Script
}

func (f *Face) shapeRun(text string, dir di.Direction, script language.Script) shaping.Output {
	runes := []rune(text)
	return f.shaper.Shape(shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: dir,
		Face:      f.font,
		Size:      fixed.Int26_6(f.size * 64),
		Script:    script,
		Language:  language.NewLanguage("en"),
	})
}

// Measure returns the size of s at this face's size, wrapping at maxWidth when
// it is positive. The wrapped size is the widest line and the stack of line
// boxes, so it matches what paint draws for the same text.
func (f *Face) Measure(s string, maxWidth float64) (w, h float64) {
	if s == "" {
		return 0, f.LineHeight()
	}
	advance := f.advance
	if maxWidth <= 0 {
		return advance(s), f.LineHeight()
	}
	lines := WrapLines(s, maxWidth, advance)
	widest := 0.0
	for _, line := range lines {
		if a := advance(line); a > widest {
			widest = a
		}
	}
	return widest, float64(len(lines)) * f.LineHeight()
}

// advance returns the width of a run, shaping it only the first time.
func (f *Face) advance(s string) float64 {
	if w, ok := f.adv[s]; ok {
		return w
	}
	w := float64(f.Shape(s).Advance) / 64
	// The bound is a memory cap, not a speed one: every entry is a prefix string
	// a wrap measured once, and the wrap caches upstream mean a page rarely asks
	// for the same prefix twice. 2048 keeps a long page fast without holding
	// thousands of dead strings per face.
	if f.adv == nil || len(f.adv) > 2048 {
		f.adv = make(map[string]float64)
	}
	f.adv[s] = w
	return w
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
	return rasterOutline(outline.Segments, scale, f.italic)
}
