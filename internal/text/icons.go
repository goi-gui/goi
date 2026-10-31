package text

import (
	"bytes"
	"sort"
	"sync"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
)

// IconNames returns every ligature the embedded Material Symbols font supports
// (`home`, `add`, `arrow_back`, …), sorted.
//
// The list is read from the font rather than vendored, so it cannot drift from
// what [Library.IconFace] can draw: replace the woff and the names follow. A
// name is lowercase ASCII with digits and underscores, so it is also a valid
// bare .goi identifier (`icon: add`).
var IconNames = sync.OnceValue(iconNames)

// iconNames walks the font's ligature substitutions. A ligature is stored as a
// glyph sequence, so a name is recovered by mapping each component glyph back
// to the character that produced it.
func iconNames() []string {
	face, err := font.ParseTTF(bytes.NewReader(materialSymbols))
	if err != nil {
		return nil
	}
	f := face.Font

	// Only printable ASCII is reachable from a ligature name; the reverse map
	// keeps the coverage scan below cheap.
	charOf := map[font.GID]rune{}
	for r := rune(' '); r < 0x7f; r++ {
		if g, ok := f.Cmap.Lookup(r); ok {
			charOf[g] = r
		}
	}
	// A ligature set is indexed by its first component's position in the
	// coverage table, which only maps glyph to index, so invert it.
	headOf := func(cov tables.Coverage, i int) (font.GID, bool) {
		for g := range charOf {
			if idx, ok := cov.Index(tables.GlyphID(g)); ok && idx == i {
				return g, true
			}
		}
		return 0, false
	}

	seen := make(map[string]bool)
	var names []string
	for _, lookup := range f.GSUB.Lookups {
		for _, sub := range lookup.Subtables {
			lig, ok := sub.(tables.LigatureSubs)
			if !ok {
				continue
			}
			for set, setDef := range lig.LigatureSets {
				head, ok := headOf(lig.Coverage, set)
				if !ok {
					continue
				}
				for _, l := range setDef.Ligatures {
					name, ok := ligatureName(charOf, head, l.ComponentGlyphIDs)
					if ok && !seen[name] {
						seen[name] = true
						names = append(names, name)
					}
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

// ligatureName spells a ligature, or reports false when a component is not a
// printable ASCII character (not an icon name then).
func ligatureName(charOf map[font.GID]rune, head font.GID, rest []tables.GlyphID) (string, bool) {
	buf := make([]byte, 0, 1+len(rest))
	for _, g := range append([]font.GID{head}, toGIDs(rest)...) {
		c, ok := charOf[g]
		if !ok {
			return "", false
		}
		buf = append(buf, byte(c))
	}
	return string(buf), true
}

func toGIDs(in []tables.GlyphID) []font.GID {
	out := make([]font.GID, len(in))
	for i, g := range in {
		out[i] = font.GID(g)
	}
	return out
}
