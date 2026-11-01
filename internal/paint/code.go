package paint

import (
	"strings"

	"github.com/ksckaan1/goi/internal/m3"
	"github.com/ksckaan1/goi/internal/tree"
)

// Code geometry.
const (
	codePad  = 12.0
	codeSize = 13.0
)

// CodeBox is the text box inside a Code block, in the same space as r.
func CodeBox(r tree.Rect) tree.Rect {
	return tree.Rect{X: r.X + codePad, Y: r.Y + codePad, W: maxf(r.W-2*codePad, 0), H: maxf(r.H-2*codePad, 0)}
}

// paintCode draws a fenced code block with syntax colours. Lines are hard
// breaks, not wrapped: a code block keeps its shape, and a line that does not
// fit is clipped at the right edge.
func (l *List) paintCode(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.fillRect(r, col.SurfaceContainerLow, m3.ShapeMedium, 0, 0, clip)
	if c.Variant.Get() == "outlined" {
		l.fillRect(r, 0, m3.ShapeMedium, col.OutlineVariant, 1, clip)
	}

	st := l.style("bodyMedium")
	st.Size = codeSize
	st.LineHeight = codeSize * 1.55
	box := CodeBox(r)
	textClip := intersect(clip, box)
	body := disabledAlpha(c, col.OnSurface)
	advance := func(s string) float64 { return textWidth(l, s, st) }

	// A tab has no advance in the font, so expand it here: Go code is indented
	// with tabs and would otherwise lose all its indentation.
	src := strings.ReplaceAll(c.Text.Get(), "\t", "    ")
	spans := HighlightCode(src, c.Language.Get())
	lineY := box.Y
	for start := 0; start <= len(src); {
		end := len(src)
		if i := indexByteFrom(src, '\n', start); i >= 0 {
			end = i
		}
		if lineY+st.LineHeight > box.Bottom() {
			break // the block is shorter than its code: clip, do not overflow
		}
		l.codeLine(src, start, end, spans, st, body, box, lineY, advance, textClip)
		lineY += st.LineHeight
		if end == len(src) {
			break
		}
		start = end + 1
	}
}

// codeLine draws one source line, splitting it into highlighted runs.
func (l *List) codeLine(src string, a, b int, spans []codeSpan, st m3.TextStyle, body uint32,
	box tree.Rect, y float64, advance func(string) float64, clip tree.Rect) {
	col := l.Theme.Colors
	x := box.X
	at := a
	for _, sp := range spans {
		if sp.End <= a || sp.Start >= b {
			continue
		}
		from, to := maxInt(sp.Start, a), minInt(sp.End, b)
		if from > at {
			l.codeRun(src[at:from], col.OnSurface, st, x, y, box, clip, advance)
			x += advance(src[at:from])
		}
		run := src[from:to]
		l.codeRun(run, codeColor(sp.Kind, col, body), st, x, y, box, clip, advance)
		x += advance(run)
		at = to
	}
	if at < b {
		l.codeRun(src[at:b], col.OnSurface, st, x, y, box, clip, advance)
	}
}

// codeRun draws one colored run of a line.
func (l *List) codeRun(s string, color uint32, st m3.TextStyle, x, y float64, box tree.Rect, clip tree.Rect, advance func(string) float64) {
	if s == "" {
		return
	}
	w := advance(s)
	if x >= box.Right() {
		return // fully past the right edge
	}
	l.textIn(s, st, color, tree.Rect{X: x, Y: y, W: w, H: st.LineHeight}, AlignStart, clip)
}

// codeColor maps a span kind to a theme role, so a scheme change recolors the
// code with the rest of the page.
func codeColor(k codeKind, col m3.Colors, body uint32) uint32 {
	switch k {
	case codeKeyword:
		return col.Primary
	case codeString:
		return col.Tertiary
	case codeComment:
		return col.OnSurfaceVariant
	case codeNumber:
		return col.Secondary
	}
	return body
}

// indexByteFrom is strings.IndexByte with a start offset.
func indexByteFrom(s string, c byte, from int) int {
	for i := from; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
