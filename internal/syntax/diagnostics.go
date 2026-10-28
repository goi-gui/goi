package syntax

import (
	"fmt"
	"sort"
	"strings"
)

// Diagnostic is a single problem found in a file.
type Diagnostic struct {
	Pos     Pos
	End     Pos
	Message string
	Hint    string // optional "did you mean" suggestion
}

// Diagnostics is a list of problems, in source order.
type Diagnostics []Diagnostic

// Error renders all diagnostics with file:line:col prefixes and a source
// snippet under each one.
func (ds Diagnostics) Error() string { return ds.Format("", nil) }

// Format renders the diagnostics, using name as the file name when non-empty
// and src (if provided) to add a snippet line.
func (ds Diagnostics) Format(name string, src []byte) string {
	var b strings.Builder
	lines := lineStarts(src)
	for i, d := range ds {
		if i > 0 {
			b.WriteByte('\n')
		}
		if name != "" {
			line, col := lines.locate(d.Pos)
			fmt.Fprintf(&b, "%s:%d:%d: %s", name, line, col, d.Message)
		} else {
			b.WriteString(d.Message)
		}
		if d.Hint != "" {
			fmt.Fprintf(&b, " (did you mean %q?)", d.Hint)
		}
		if len(src) > 0 {
			if snippet := lines.snippet(src, d.Pos); snippet != "" {
				b.WriteByte('\n')
				b.WriteString(snippet)
			}
		}
	}
	return b.String()
}

// Empty reports whether there are no diagnostics.
func (ds Diagnostics) Empty() bool { return len(ds) == 0 }

type lineIndex struct {
	starts []int
}

func lineStarts(src []byte) lineIndex {
	idx := lineIndex{starts: []int{0}}
	for i, c := range src {
		if c == '\n' {
			idx.starts = append(idx.starts, i+1)
		}
	}
	return idx
}

// locate converts a byte offset to a 1-based line and column.
func (l lineIndex) locate(p Pos) (line, col int) {
	if len(l.starts) == 0 {
		return 1, 1
	}
	off := int(p)
	i := sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > off }) - 1
	if i < 0 {
		i = 0
	}
	return i + 1, off - l.starts[i] + 1
}

// snippet returns the source line containing p with a caret under the column.
func (l lineIndex) snippet(src []byte, p Pos) string {
	if len(l.starts) == 0 || int(p) > len(src) {
		return ""
	}
	off := int(p)
	i := sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > off }) - 1
	if i < 0 {
		i = 0
	}
	start := l.starts[i]
	end := len(src)
	if i+1 < len(l.starts) {
		end = l.starts[i+1] - 1
	}
	line := strings.TrimRight(string(src[start:end]), "\r\n")
	if len(line) > 200 {
		line = line[:200]
	}
	col := off - start
	if col > len(line) {
		col = len(line)
	}
	// Preserve tabs so the caret lines up in editors that show the raw line.
	pad := make([]byte, col)
	for j := range pad {
		if line[j] == '\t' {
			pad[j] = '\t'
		} else {
			pad[j] = ' '
		}
	}
	return "  " + line + "\n  " + string(pad) + "^"
}

// Suggest returns the closest candidate to name, or "" when nothing is close
// enough. Distance is capped so unrelated words never suggest each other.
func Suggest(name string, candidates []string) string {
	best, bestDist := "", 3 // allow at most 2 edits
	lower := strings.ToLower(name)
	for _, c := range candidates {
		d := EditDistance(lower, strings.ToLower(c))
		if d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

func EditDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
