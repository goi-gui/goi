package text

import (
	"github.com/go-text/typesetting/language"
	"golang.org/x/text/unicode/bidi"
)

// splitBidi cuts a line into runs that share a direction. Script is taken from
// the first strong rune of the run, which is what picks Arabic shaping for an
// Arabic stretch without any language hint from the app.
func splitBidi(s string) []bidiRun {
	if s == "" {
		return nil
	}
	var p bidi.Paragraph
	if _, err := p.SetString(s); err != nil {
		return []bidiRun{{text: s, script: language.Latin}}
	}
	ord, err := p.Order()
	if err != nil {
		return []bidiRun{{text: s, script: language.Latin}}
	}
	runs := make([]bidiRun, 0, ord.NumRuns())
	for i := 0; i < ord.NumRuns(); i++ {
		r := ord.Run(i)
		start, end := r.Pos()
		// Pos returns rune indices; convert to a byte slice of the original.
		text := runeSlice(s, start, end+1)
		if text == "" {
			continue
		}
		runs = append(runs, bidiRun{
			text:   text,
			rtl:    r.Direction() == bidi.RightToLeft,
			script: scriptOf(text),
		})
	}
	if len(runs) == 0 {
		return []bidiRun{{text: s, script: language.Latin}}
	}
	return runs
}

// baseDirection reports whether a line is predominantly right to left.
func baseDirection(s string) bool {
	var p bidi.Paragraph
	if _, err := p.SetString(s); err != nil {
		return false
	}
	ord, err := p.Order()
	if err != nil {
		return false
	}
	return ord.Direction() == bidi.RightToLeft
}

// scriptOf returns the script of the first strong rune, defaulting to Latin.
func scriptOf(s string) language.Script {
	for _, r := range s {
		prop, _ := bidi.LookupRune(r)
		switch prop.Class() {
		case bidi.L, bidi.R, bidi.AL:
			// A strong class decides the script; punctuation and digits do not.
			return language.LookupScript(r)
		}
	}
	return language.Latin
}

// runeSlice returns the text between two rune indices.
func runeSlice(s string, start, end int) string {
	rs := []rune(s)
	if start < 0 {
		start = 0
	}
	if end > len(rs) {
		end = len(rs)
	}
	if start >= end {
		return ""
	}
	return string(rs[start:end])
}
