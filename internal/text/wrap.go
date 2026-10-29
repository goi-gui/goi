package text

import "strings"

// WrapLines breaks s into lines no wider than maxWidth, using greedy word
// wrapping. advance returns the width of a run at the same style.
//
// Explicit newlines always break. Runs of spaces collapse to one, and a single
// word wider than maxWidth is broken between runes, so text never runs past the
// column. That is right for UI labels, not for preformatted text.
func WrapLines(s string, maxWidth float64, advance func(string) float64) []string {
	if maxWidth <= 0 {
		return []string{s}
	}
	if s == "" {
		return []string{""}
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
	return out
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
