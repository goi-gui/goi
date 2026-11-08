package paint

import (
	"github.com/goi-gui/goi/internal/highlight"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// Code geometry.
const (
	codePad  = 12.0
	codeSize = 13.0
	// codeCopySize is the hit area of the copy button in the corner.
	codeCopySize = 32.0
)

// CodeBox is the text box inside a Code block, in the same space as r.
func CodeBox(r tree.Rect) tree.Rect {
	return tree.Rect{X: r.X + codePad, Y: r.Y + codePad, W: maxf(r.W-2*codePad, 0), H: maxf(r.H-2*codePad, 0)}
}

// CodeCopyRect is the copy button's box, which is also its hit area. The input
// layer reads it so a click lands where the glyph is drawn.
func CodeCopyRect(r tree.Rect) tree.Rect {
	return tree.Rect{
		X: r.Right() - 8 - codeCopySize,
		Y: r.Y + 8,
		W: codeCopySize,
		H: codeCopySize,
	}
}

// CodeStyle is the style a Code block draws with. The input layer measures with
// it too, so a click cannot drift from the glyphs.
func CodeStyle() m3.TextStyle {
	st := m3.RoleStyle("bodyMedium")
	st.Size = codeSize
	st.LineHeight = codeSize * 1.55
	return st
}

// CodeLines splits a code block's source on hard line breaks. A code block does
// not wrap: it keeps its shape, and a line that does not fit is clipped.
func CodeLines(src string) []text.Line {
	var out []text.Line
	from := 0
	for i := 0; i <= len(src); i++ {
		if i == len(src) || src[i] == '\n' {
			out = append(out, text.Line{Text: src[from:i], Start: from, End: i})
			from = i + 1
		}
	}
	return out
}

// paintCode draws a fenced code block with syntax colours, the reader's
// selection underneath, and the copy button when the design asks for it.
func (l *List) paintCode(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.fillRect(r, col.SurfaceContainerLow, m3.ShapeMedium, 0, 0, clip)
	if c.Variant.Get() == "outlined" {
		l.fillRect(r, 0, m3.ShapeMedium, col.OutlineVariant, 1, clip)
	}

	st := CodeStyle()
	box := CodeBox(r)
	textClip := intersect(clip, box)
	body := disabledAlpha(c, col.OnSurface)
	advance := func(s string) float64 { return textWidth(l, s, st) }

	code, _ := c.Node().(*tree.Code)
	if code == nil {
		return
	}
	src, spans := code.Source(), code.Spans()
	lines := CodeLines(src)

	selA, selB := code.SelA, code.SelB
	if selA > selB {
		selA, selB = selB, selA
	}
	for i, ln := range lines {
		y := box.Y + float64(i)*st.LineHeight
		if y+st.LineHeight > box.Bottom()+1 {
			break // the block is shorter than its code: clip, do not overflow
		}
		if selA != selB && ln.End > selA && ln.Start < selB {
			l.codeSelection(ln, selA, selB, st, box, y, advance, textClip)
		}
		l.codeLine(src, ln, spans, st, body, box, y, advance, textClip)
	}

	if c.ShowCopy.Get() {
		btn := CodeCopyRect(r)
		l.fillRect(btn, withAlpha(col.SecondaryContainer, 0.9), m3.ShapeSmall, 0, 0, clip)
		// The copy button is one small press target inside the block, so its
		// ripple is clipped to the button rather than to the whole Code box.
		l.ripple(c, btn, m3.ShapeSmall, col.OnSecondaryContainer, intersect(clip, btn))
		l.iconIn("content_copy", 18, col.OnSecondaryContainer, btn.X+btn.W/2, btn.Y+btn.H/2, clip)
	}
}

// codeSelection draws the highlight for a byte range on one line.
func (l *List) codeSelection(ln text.Line, a, b int, st m3.TextStyle, box tree.Rect, y float64,
	advance func(string) float64, clip tree.Rect,
) {
	from := clampInt(a-ln.Start, 0, len(ln.Text))
	to := clampInt(b-ln.Start, 0, len(ln.Text))
	if from == to {
		return
	}
	x0 := advance(ln.Text[:from])
	x1 := advance(ln.Text[:to])
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:   [4]float32{float32(box.X + x0), float32(y + st.LineHeight*0.1), float32(x1 - x0), float32(st.LineHeight * 0.8)},
		Radius: [4]float32{2, 2, 2, 2},
		Color:  withAlpha(l.Theme.Colors.Primary, m3.SelectionOpacity),
		Clip:   clipArr(clip),
	})
}

// codeLine draws one source line, splitting it into highlighted runs.
func (l *List) codeLine(src string, ln text.Line, spans []highlight.Span, st m3.TextStyle, body uint32,
	box tree.Rect, y float64, advance func(string) float64, clip tree.Rect,
) {
	col := l.Theme.Colors
	x := box.X
	at := ln.Start
	for _, sp := range spans {
		if sp.End <= ln.Start || sp.Start >= ln.End {
			continue
		}
		from, to := maxInt(sp.Start, ln.Start), minInt(sp.End, ln.End)
		if from > at {
			l.codeRun(src[at:from], col.OnSurface, st, x, y, box, clip, advance)
			x += advance(src[at:from])
		}
		run := src[from:to]
		l.codeRun(run, codeColor(sp.Kind, col, body), st, x, y, box, clip, advance)
		x += advance(run)
		at = to
	}
	if at < ln.End {
		l.codeRun(src[at:ln.End], col.OnSurface, st, x, y, box, clip, advance)
	}
}

// codeRun draws one colored run of a line.
func (l *List) codeRun(s string, color uint32, st m3.TextStyle, x, y float64, box tree.Rect, clip tree.Rect, advance func(string) float64) {
	if s == "" {
		return
	}
	if x >= box.Right() {
		return // fully past the right edge
	}
	l.textIn(s, st, color, tree.Rect{X: x, Y: y, W: advance(s), H: st.LineHeight}, AlignStart, clip)
}

// codeColor maps a span kind to a theme role, so a scheme change recolors the
// code with the rest of the page.
func codeColor(k highlight.Kind, col m3.Colors, body uint32) uint32 {
	switch k {
	case highlight.Keyword, highlight.Function:
		return col.Primary
	case highlight.String, highlight.Attribute:
		return col.Tertiary
	case highlight.Number, highlight.Type:
		return col.Secondary
	case highlight.Comment, highlight.Operator:
		return col.OnSurfaceVariant
	}
	return body
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
