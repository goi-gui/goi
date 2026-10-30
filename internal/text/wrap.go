package text

import "strings"

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
