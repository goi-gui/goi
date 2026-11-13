package text

import "math"

// Inline is one styled run of a paragraph. Style is opaque to this package: it
// is the caller's index, handed back on every measure call.
type Inline struct {
	Text  string
	Style int
}

// InlineRun is one run placed on a line, in the line's own space: X is where it
// starts and W is its advance.
type InlineRun struct {
	Text  string
	Style int
	X, W  float64
}

// inlineToken is a word or a run of spaces, split at run boundaries. A word
// keeps the runs it came from, so `bo`+`ld` in two styles still moves to the
// next line as one word.
type inlineToken struct {
	space bool
	brk   bool
	parts []Inline
}

// inlineTokens cuts runs at whitespace, newlines and style boundaries.
func inlineTokens(runs []Inline) []inlineToken {
	var out []inlineToken
	add := func(space bool, p Inline) {
		if p.Text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].space == space && !out[n-1].brk {
			out[n-1].parts = append(out[n-1].parts, p)
			return
		}
		out = append(out, inlineToken{space: space, parts: []Inline{p}})
	}
	for _, r := range runs {
		s := r.Text
		for i := 0; i < len(s); {
			if s[i] == '\n' {
				out = append(out, inlineToken{brk: true})
				i++
				continue
			}
			space := s[i] == ' ' || s[i] == '\t'
			j := i
			for j < len(s) && s[j] != '\n' && (s[j] == ' ' || s[j] == '\t') == space {
				j++
			}
			add(space, Inline{Text: s[i:j], Style: r.Style})
			i = j
		}
	}
	return out
}

// WrapInline lays styled runs out as a paragraph of the given width and returns
// its lines. A word may straddle runs: the split is by whitespace, so `bo`+`ld`
// in two styles stays one word and moves to the next line together. A word
// wider than the column is cut, so the text never escapes its box. A newline in
// a run forces a break.
//
// measure reports the advance of a run in its style, so the caller decides what
// a style means.
func WrapInline(runs []Inline, width float64, measure func(style int, s string) float64) [][]InlineRun {
	if width <= 0 {
		width = math.Inf(1)
	}
	var lines [][]InlineRun
	var line []InlineRun
	lineW := 0.0
	var pending []Inline
	pendingW := 0.0

	appendRun := func(s string, style int) {
		if s == "" {
			return
		}
		w := measure(style, s)
		if n := len(line); n > 0 && line[n-1].Style == style {
			line[n-1].Text += s
			line[n-1].W += w
		} else {
			line = append(line, InlineRun{Text: s, Style: style, X: lineW, W: w})
		}
		lineW += w
	}
	flush := func() {
		lines = append(lines, line)
		line, lineW = nil, 0
		pending, pendingW = nil, 0
	}
	// A space is held back until the word after it fits, so a line never ends
	// with trailing whitespace and a wrap does not cost a space of width.
	place := func(parts []Inline) {
		total := 0.0
		for _, p := range parts {
			total += measure(p.Style, p.Text)
		}
		if len(line) > 0 && lineW+pendingW+total > width {
			flush()
		}
		for _, p := range pending {
			appendRun(p.Text, p.Style)
		}
		pending, pendingW = nil, 0
		for _, p := range parts {
			w := measure(p.Style, p.Text)
			if lineW > 0 && lineW+w > width {
				flush()
			}
			// A run wider than the whole column has to be cut, piece by piece,
			// so a long word or a URL still wraps.
			for w > width && len([]rune(p.Text)) > 1 {
				head, tail := splitToWidth(p.Text, width, func(t string) float64 { return measure(p.Style, t) })
				if head == "" {
					break
				}
				appendRun(head, p.Style)
				flush()
				p.Text = tail
				w = measure(p.Style, tail)
			}
			appendRun(p.Text, p.Style)
		}
	}

	for _, tok := range inlineTokens(runs) {
		switch {
		case tok.brk:
			flush()
		case tok.space:
			for _, p := range tok.parts {
				pending = append(pending, p)
				pendingW += measure(p.Style, p.Text)
			}
		default:
			place(tok.parts)
		}
	}
	if len(line) > 0 {
		flush()
	}
	if len(lines) == 0 {
		// An empty paragraph still takes a line, so a blank block keeps its
		// height instead of collapsing.
		lines = append(lines, nil)
	}
	return lines
}
