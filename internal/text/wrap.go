package text

import (
	"strings"
	"unicode/utf8"
)

// WrapLines breaks s into lines no wider than maxWidth, using greedy word
// wrapping. advance returns the width of a run at the same style.
//
// Explicit newlines always break. Runs of spaces collapse to one, and a single
// word wider than maxWidth is broken between runes, so text never runs past the
// column. That is right for UI labels, not for preformatted text.
func WrapLines(s string, maxWidth float64, advance func(string) float64) []string {
	lines := WrapRanges(s, maxWidth, advance)
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = ln.Text
	}
	return out
}

// Line is one wrapped line and the byte range it came from. Start and End make
// a selection reach the source string; a range is empty for a line whose text
// collapsed away entirely.
type Line struct {
	Text       string
	Start, End int
}

// WrapRanges is WrapLines with the source offsets kept, which a selectable Text
// needs to turn a pointer position back into a byte index.
func WrapRanges(s string, maxWidth float64, advance func(string) float64) []Line {
	if maxWidth <= 0 {
		return []Line{{Text: s, Start: 0, End: len(s)}}
	}
	if s == "" {
		return []Line{{Text: "", Start: 0, End: 0}}
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, w := range words {
			switch {
			case line == "":
				line = w
			case advance(line+" "+w) <= maxWidth:
				line += " " + w
			default:
				out = append(out, line)
				line = w
			}
			// A single word can still be wider than the column.
			for advance(line) > maxWidth && len([]rune(line)) > 1 {
				head, tail := splitToWidth(line, maxWidth, advance)
				out = append(out, head)
				line = tail
			}
		}
		out = append(out, line)
	}
	// Word wrapping drops whitespace, so find each line in the source in order;
	// that keeps the offsets exact for ordinary prose and never runs backwards.
	lines := make([]Line, 0, len(out))
	from := 0
	for _, text := range out {
		idx := strings.Index(s[from:], text)
		if idx < 0 {
			idx = 0
		}
		start := from + idx
		lines = append(lines, Line{Text: text, Start: start, End: start + len(text)})
		from = start + len(text)
	}
	return lines
}

// splitToWidth cuts one line into the longest rune prefix that fits and the
// rest. It always makes progress, so the caller's loop terminates.
func splitToWidth(line string, maxWidth float64, advance func(string) float64) (string, string) {
	runes := []rune(line)
	n := len(runes) - 1
	for n > 0 && advance(string(runes[:n])) > maxWidth {
		n--
	}
	return string(runes[:n]), string(runes[n:])
}

// WrapEdit breaks s into lines for an editor, keeping every byte of the source:
// nothing is trimmed or collapsed, a hard newline always ends a line, and the
// Start/End offsets are exact. WrapRanges is for labels, where dropping the
// whitespace is what you want; a caret needs the other behaviour.
func WrapEdit(s string, maxWidth float64, advance func(string) float64) []Line {
	var out []Line
	from := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			out = append(out, wrapParagraph(s, from, i, maxWidth, advance)...)
			from = i + 1
		}
	}
	return out
}

// wrapParagraph wraps s[a:b], which contains no newline, into lines that keep
// their offsets. A break lands after the last space that fits so the space stays
// on the line it belongs to; a word longer than the column breaks between runes.
func wrapParagraph(s string, a, b int, maxWidth float64, advance func(string) float64) []Line {
	if a >= b {
		return []Line{{Text: "", Start: a, End: a}}
	}
	if maxWidth <= 0 {
		return []Line{{Text: s[a:b], Start: a, End: b}}
	}
	var out []Line
	for a < b {
		end := b
		space := -1
		for i := a; i < b; {
			_, size := utf8.DecodeRuneInString(s[i:])
			if !fitsIn(s[a:i+size], maxWidth, advance) {
				// Break after the last space if there is one past the start;
				// otherwise break here, between runes.
				if space > a {
					end = space + 1
				} else {
					end = i
					if end == a {
						end = i + size // never stall on a single wide rune
					}
				}
				break
			}
			if s[i] == ' ' || s[i] == '\t' {
				space = i
			}
			i += size
		}
		out = append(out, Line{Text: s[a:end], Start: a, End: end})
		a = end
	}
	return out
}

// fitsIn reports whether text fits the column. advance is called at most once
// per candidate, so wrapping a paragraph stays linear in its runes.
func fitsIn(text string, maxWidth float64, advance func(string) float64) bool {
	return advance(text) <= maxWidth
}
