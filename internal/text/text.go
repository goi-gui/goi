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
	tag    uint16 // font identity, so atlas keys never collide across fonts
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
	// runs caches ShapeRuns, which is what a repaint asks for every text op.
	// Without it every frame re-shapes every visible line: Shape's cache never
	// sees the call, because the renderer needs the per-face split.
	runs map[string][]ShapedRun
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
	// families holds the app's own fonts, keyed by the name a design writes in
	// `fontFamily`. One file serves every weight; familyTag is the atlas tag it
	// was given, so its glyphs cannot collide with Roboto's.
	families  map[string]familyFace
	familyTag uint16
	icon      *font.Face
	// iconFilled is the same family with the FILL axis at 1, the face
	// `filledIcon: true` draws with.
	iconFilled *font.Face
	// emoji is Noto Color Emoji, the face a rune Roboto has no glyph for falls
	// back to; emojiCache holds its decoded bitmaps per size and glyph.
	emoji      *font.Face
	emojiCache map[emojiKey]*emojiGlyph
	emojiOnce  sync.Once
	emojiErr   error
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
	filled bool // the filled Material Symbols instance
	italic bool
	emoji  bool
	// family is the registered name, so two families at one size get their own
	// face and atlas tag.
	family string
}

// familyFace is one registered family. face is the fallback for a family with
// one file; variants holds the per-weight and per-style files that a
// fontFamily plus fontWeight/fontStyle selects.
type familyFace struct {
	face     *font.Face
	tag      uint16
	variants map[familyVariant]familyVariantFace
}

type familyVariant struct {
	weight Weight
	italic bool
}

type familyVariantFace struct {
	face   *font.Face
	tag    uint16
	italic bool // the file itself is italic, so it is not slanted again
}

// pick chooses the exact variant, then the nearest weight in the requested
// style, then the nearest upright face. The caller adds the synthetic slant
// when an upright file answers an italic request.
func (f familyFace) pick(weight Weight, italic bool) (familyVariantFace, bool) {
	if v, ok := f.variants[familyVariant{weight: weight, italic: italic}]; ok {
		return v, true
	}
	best := familyVariantFace{}
	bestDiff := Weight(1 << 30)
	for v, face := range f.variants {
		if v.italic != italic {
			continue
		}
		diff := v.weight - weight
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			best, bestDiff = face, diff
		}
	}
	if best.face != nil {
		return best, true
	}
	if italic {
		// No italic file: use the upright one at the nearest weight and let the
		// rasterizer slant it.
		if v, ok := f.variants[familyVariant{weight: weight}]; ok {
			return v, true
		}
		for v, face := range f.variants {
			if v.italic {
				continue
			}
			diff := v.weight - weight
			if diff < 0 {
				diff = -diff
			}
			if diff < bestDiff {
				best, bestDiff = face, diff
			}
		}
		if best.face != nil {
			return best, true
		}
	}
	if f.face != nil {
		return familyVariantFace{face: f.face, tag: f.tag}, true
	}
	return familyVariantFace{}, false
}

// faceSlot is one entry of the direct-mapped face cache.
type faceSlot struct {
	key faceKey
	f   *Face
}

// NewLibrary returns a library with Roboto and Material Symbols loaded.
func NewLibrary() *Library {
	l := &Library{
		fonts:     map[Weight]*font.Face{},
		families:  map[string]familyFace{},
		familyTag: 16, // 0..15 are the built-in weights and the icon font
		faces:     map[faceKey]*Face{},
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
	if f, err := font.ParseTTF(bytes.NewReader(materialSymbolsFilled)); err == nil {
		l.iconFilled = f
	}
	// A monospace family for terminals and code-like content. It is a built-in
	// family, so a design writes `fontFamily: "Roboto Mono"` like any other.
	if len(robotoMono) > 0 {
		if f, err := font.ParseTTF(bytes.NewReader(robotoMono)); err == nil {
			l.families["Roboto Mono"] = familyFace{face: f, tag: l.familyTag}
			l.familyTag += 2
		}
	}
	return l
}

// ensureEmoji parses the embedded colour font once. A failure is not fatal:
// text falls back to the monochrome glyphs of the regular face.
func (l *Library) ensureEmoji() {
	l.emojiOnce.Do(func() {
		if err := l.loadEmoji(notoEmoji); err != nil {
			l.emojiErr = err
		}
	})
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

// RegisterFamily adds an app font under name, which is what a design's
// `fontFamily` writes. The file serves every weight: a single face cannot vary
// its weight, and a heavier one falls back to the same outlines.
func (l *Library) RegisterFamily(name string, data []byte) error {
	if name == "" {
		return fmt.Errorf("text: a family needs a name")
	}
	f, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("text: parse font %q: %w", name, err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fam := l.families[name]
	if fam.tag == 0 {
		if l.familyTag >= 254 {
			return fmt.Errorf("text: too many font families")
		}
		fam.tag = l.familyTag
		l.familyTag += 2 // the odd tag is the slanted copy
	}
	fam.face = f
	l.families[name] = fam
	l.dropFamilyLocked(name)
	return nil
}

// RegisterFamilyFace adds one weight/style file to a family. A family can hold
// several files; a design still names only the family and picks the file with
// fontWeight and fontStyle.
func (l *Library) RegisterFamilyFace(name string, weight Weight, italic bool, data []byte) error {
	if name == "" {
		return fmt.Errorf("text: a family needs a name")
	}
	f, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("text: parse font %q: %w", name, err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fam := l.families[name]
	if fam.variants == nil {
		fam.variants = map[familyVariant]familyVariantFace{}
	}
	key := familyVariant{weight: weight, italic: italic}
	if vf, ok := fam.variants[key]; ok {
		vf.face = f
		fam.variants[key] = vf
	} else {
		if l.familyTag >= 254 {
			return fmt.Errorf("text: too many font faces")
		}
		fam.variants[key] = familyVariantFace{face: f, tag: l.familyTag, italic: italic}
		l.familyTag += 2
	}
	l.families[name] = fam
	l.dropFamilyLocked(name)
	return nil
}

// RegisterVariableFamily registers one variable font under a family and builds a
// face for every 100-step weight. The design still uses fontWeight to pick the
// weight; wght is supplied here, while axes pins the other axes (for example
// wdth or opsz). Italic requests slant the upright face, since the ital/slnt
// axis is not driven separately.
func (l *Library) RegisterVariableFamily(name string, data []byte, axes map[string]float64) error {
	if name == "" {
		return fmt.Errorf("text: a family needs a name")
	}
	parsed, vars, err := variableSetup(data, axes)
	if err != nil {
		return fmt.Errorf("text: variable font %q: %w", name, err)
	}
	// The file's own style decides which slot it fills: the italic file of a
	// family registers the italic variants, so fontStyle: italic uses its
	// outlines instead of slanting the upright file a second time.
	italic := parsed.Describe().Aspect.Style == font.StyleItalic
	l.mu.Lock()
	defer l.mu.Unlock()
	fam := l.families[name]
	if fam.variants == nil {
		fam.variants = map[familyVariant]familyVariantFace{}
	}
	for weight := 100; weight <= 900; weight += 100 {
		face := font.NewFace(parsed.Font)
		face.SetVariations(append(vars, font.Variation{Tag: weightTag, Value: float32(weight)}))
		key := familyVariant{weight: Weight(weight), italic: italic}
		if vf, ok := fam.variants[key]; ok {
			vf.face = face
			fam.variants[key] = vf
			continue
		}
		if l.familyTag >= 254 {
			return fmt.Errorf("text: too many font faces")
		}
		fam.variants[key] = familyVariantFace{face: face, tag: l.familyTag, italic: italic}
		l.familyTag += 2
	}
	l.families[name] = fam
	l.dropFamilyLocked(name)
	return nil
}

// ValidateVariableFont parses data and checks that it is a variable font whose
// outlines can be read. An app can call it before a window exists, so a bad
// file is reported by LoadVariableFont rather than at the first frame.
func ValidateVariableFont(data []byte, axes map[string]float64) error {
	_, _, err := variableSetup(data, axes)
	return err
}

// variableSetup parses a variable font, pins the extra axes and checks that at
// least the default face has usable outlines.
func variableSetup(data []byte, axes map[string]float64) (*font.Face, []font.Variation, error) {
	parsed, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("parse: %w", err)
	}
	vars := make([]font.Variation, 0, len(axes)+1)
	for axis, value := range axes {
		if axis == "wght" {
			continue
		}
		tag, ok := variationTag(axis)
		if !ok {
			return nil, nil, fmt.Errorf("variation axis %q must be four characters", axis)
		}
		vars = append(vars, font.Variation{Tag: tag, Value: float32(value)})
	}
	probe := font.NewFace(parsed.Font)
	probe.SetVariations(append(vars, font.Variation{Tag: weightTag, Value: float32(WeightRegular)}))
	if len(probe.Coords()) == 0 {
		return nil, nil, fmt.Errorf("not a variable font")
	}
	if !hasOutlines(probe) {
		// Some CFF2 variable fonts parse but produce no outline; without this
		// check the app would draw blank text and report nothing.
		return nil, nil, fmt.Errorf("no readable outlines (some CFF2 variable fonts are unsupported)")
	}
	return parsed, vars, nil
}

var weightTag = font.Tag(uint32('w')<<24 | uint32('g')<<16 | uint32('h')<<8 | uint32('t'))

// hasOutlines reports whether the Latin glyphs that the font maps have readable
// outlines. A font with no Latin coverage passes, since another script may be
// its only content.
func hasOutlines(f *font.Face) bool {
	mapped := false
	for _, r := range "Aa0" {
		gid, ok := f.NominalGlyph(r)
		if !ok {
			continue
		}
		mapped = true
		if out, ok := f.GlyphData(gid).(font.GlyphOutline); ok && len(out.Segments) > 0 {
			return true
		}
	}
	return !mapped
}

// variationTag turns a four-character axis name into an OpenType tag.
func variationTag(name string) (font.Tag, bool) {
	if len(name) != 4 {
		return 0, false
	}
	return font.Tag(uint32(name[0])<<24 | uint32(name[1])<<16 | uint32(name[2])<<8 | uint32(name[3])), true
}

// dropFamilyLocked clears built faces after a family file changes, so the next
// frame reshapes instead of using stale outlines.
func (l *Library) dropFamilyLocked(name string) {
	for k := range l.faces {
		if k.family == name {
			delete(l.faces, k)
		}
	}
	l.front = [4]faceSlot{}
}

// HasFamily reports whether name was registered.
func (l *Library) HasFamily(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.families[name]
	return ok
}

// Atlas returns the glyph atlas, for the renderer to upload.
func (l *Library) Atlas() *Atlas { return l.atlas }

// Face returns the face for a size and weight, creating it once.
func (l *Library) Face(size float64, weight Weight) *Face {
	return l.face(size, weight, false, false, false, "")
}

// FamilyFace returns the face of a registered family, falling back to Roboto
// when the name is unknown or empty, so a design that names a font the app never
// loaded still draws.
func (l *Library) FamilyFace(size float64, weight Weight, family string) *Face {
	return l.face(size, weight, false, false, false, family)
}

// FamilyItalicFace is FamilyFace slanted for `fontStyle: italic`.
func (l *Library) FamilyItalicFace(size float64, weight Weight, family string) *Face {
	return l.face(size, weight, false, false, true, family)
}

// ItalicFace returns the same face slanted for `fontStyle: italic`. Roboto has
// no italic file embedded, so the slant is synthesized at raster time.
func (l *Library) ItalicFace(size float64, weight Weight) *Face {
	return l.face(size, weight, false, false, true, "")
}

// IconFace returns the face for Material Symbols at a size.
func (l *Library) IconFace(size float64) *Face {
	return l.face(size, WeightRegular, true, false, false, "")
}

// FilledIconFace is IconFace with the FILL axis at 1, the glyph `filledIcon:
// true` asks for.
func (l *Library) FilledIconFace(size float64) *Face {
	return l.face(size, WeightRegular, true, true, false, "")
}

// HasIcon reports whether the face resolves name to one ligature glyph. A name
// the font does not carry shapes to the letters it is spelled with, which is
// what tells the filled instance (a newer icon set) to fall back to the
// outlined one.
func (f *Face) HasIcon(name string) bool {
	if f == nil || name == "" {
		return false
	}
	n := 0
	for _, r := range f.ShapeRuns(name) {
		n += len(r.Output.Glyphs)
		if n > 1 {
			return false
		}
	}
	return n == 1
}

func (l *Library) face(size float64, weight Weight, icon, filled, italic bool, family string) *Face {
	if size <= 0 {
		size = 16
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if icon {
		italic = false
		family = ""
	} else {
		filled = false
		if _, ok := l.families[family]; !ok {
			// A custom family keeps the declared weight so 600 can choose a
			// SemiBold file; Roboto snaps to the five faces it ships.
			weight = NearestWeight(weight)
		}
	}
	key := faceKey{weight: weight, size: int(size * 4), icon: icon, filled: filled, italic: italic, family: family}
	slot := &l.front[(key.size^int(weight)*7^boolInt(italic)*5^boolInt(filled)*11)&3]
	if slot.f != nil && slot.key == key {
		return slot.f
	}

	var src *font.Face
	var tag uint16
	syntheticItalic := italic
	if fam, ok := l.families[family]; ok && !icon {
		if vf, ok := fam.pick(weight, italic); ok {
			src, tag = vf.face, vf.tag
			if vf.italic {
				// The file already has the italic outlines.
				syntheticItalic = false
			} else if italic {
				// Keep slanted glyphs in their own atlas entries.
				tag++
			}
		}
	} else if icon && filled {
		src = l.iconFilled
		tag = 6
		if src == nil {
			// A build without the filled instance still draws the icon.
			src, tag = l.icon, 3
		}
	} else if icon {
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
	f := &Face{font: src, tag: tag, size: size, upem: float32(src.Upem()), italic: syntheticItalic, lib: l}
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
func (f *Face) Tag() uint16 { return f.tag }

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
	if out, ok := f.runs[s]; ok {
		return out
	}
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
	if f.runs == nil || len(f.runs) >= maxShapedRuns {
		f.runs = make(map[string][]ShapedRun)
	}
	// The slice and its glyphs are read-only to every caller, the same contract
	// f.shaped has.
	f.runs[s] = out
	return out
}

// splitFont cuts a line where the face changes: ordinary text stays with this
// face, and a rune it does not cover goes to the emoji face.
func (f *Face) splitFont(s string) []fontSeg {
	if f.lib == nil || f.emoji {
		return []fontSeg{{text: s}}
	}
	// The first emoji a process draws is what loads the font; asking only
	// whether it is already parsed would miss that line.
	f.lib.ensureEmoji()
	if f.lib.emoji == nil {
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
