package paint

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// TextArea geometry. It is exported because the input layer maps a pointer to a
// caret with the same numbers: paint and hit testing must not drift.
const (
	textAreaPad    = 16.0
	textAreaLabel  = 34.0 // band the floating label sits in
	textAreaBottom = 12.0
)

// TextFieldBox is the field's own height inside a node that may reserve a line
// for supporting text under it. The node is the whole component, so the field
// fill, border and text stop above the helper line.
func TextFieldBox(r tree.Rect, supporting bool) tree.Rect {
	if supporting && r.H > 20 {
		r.H -= 20
	}
	return r
}

// TextAreaBox is the visible text box inside a TextArea, in the same coordinate
// space as r. supporting shrinks the field so its helper line has room.
func TextAreaBox(r tree.Rect, supporting bool) tree.Rect {
	r = TextFieldBox(r, supporting)
	return tree.Rect{
		X: r.X + textAreaPad,
		Y: r.Y + textAreaLabel,
		W: maxf(r.W-2*textAreaPad, 0),
		H: maxf(r.H-textAreaLabel-textAreaBottom, 0),
	}
}

// TextAreaLines wraps a value to a box with the painter's advance, so a caret
// or a selection lands on the glyph it belongs to.
func TextAreaLines(src string, width float64, advance func(string) float64) []text.Line {
	return text.WrapEdit(src, width, advance)
}

func (l *List) paintTextArea(c *tree.Component, r tree.Rect, clip tree.Rect) {
	r = TextFieldBox(r, c.Supporting.Get() != "")
	col := l.Theme.Colors
	filled := variant(c, "filled") == "filled"
	fg := disabledAlpha(c, col.OnSurface)
	hint := disabledAlpha(c, col.OnSurfaceVariant)
	container := col.SurfaceContainerHighest
	border := col.Outline
	bw := 1.0
	if c.Disabled.Get() {
		container = withAlpha(container, m3.DisabledContainerOpacity)
		border = withAlpha(border, m3.DisabledContainerOpacity)
	}

	radius := m3.ShapeExtraSmall
	if filled {
		l.fillRect(r, container, radius, 0, 0, clip)
		ind := col.OnSurfaceVariant
		th := 1.0
		if c.Focused {
			ind, th = col.Primary, 2.0
		}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect,
			Rect:  [4]float32{float32(r.X), float32(r.Bottom() - th), float32(r.W), float32(th)},
			Color: ind, Clip: clipArr(clip),
		})
	} else {
		if c.Focused {
			border, bw = col.Primary, 2
		}
		l.fillRect(r, 0, radius, border, bw, clip)
	}

	body := l.style("bodyLarge")
	small := l.style("bodySmall")
	lh := body.LineHeight
	if lh <= 0 {
		lh = body.Size * 1.2
	}

	box := TextAreaBox(r, false)
	hasValue := c.Text.Get() != "" || c.Preedit != "" || c.Focused
	if hasValue {
		// Content or focus floats the label into the top band; the text line
		// below is free for the value or an empty-field placeholder.
		l.textIn(c.Label.Get(), small, hint,
			tree.Rect{X: box.X, Y: r.Y + 8, W: box.W, H: 16}, AlignStart, clip)
	} else {
		// Empty and unfocused: one line, and a label wins over a placeholder so
		// the two never draw on top of each other.
		label := c.Label.Get()
		if label == "" {
			label = c.Placeholder.Get()
		}
		l.textIn(label, body, hint,
			tree.Rect{X: box.X, Y: box.Y, W: box.W, H: lh}, AlignStart, clip)
	}

	lines := TextAreaLines(c.Text.Get(), box.W, func(s string) float64 {
		return textWidth(l, s, body)
	})
	// The scroll keeps the caret line inside the box. It lives in TextScroll so
	// input can subtract it when mapping a pointer back to a line.
	c.TextScroll = areaScroll(c, lines, box, lh)
	shift := c.TextScroll
	textClip := intersect(clip, box)

	if c.Focused {
		if a, b, ok := c.Selection(); ok {
			l.areaSelection(lines, c.Text.Get(), a, b, box, body, lh, shift, textClip)
		}
	}
	for i, ln := range lines {
		y := box.Y + float64(i)*lh - shift
		if y+lh < box.Y || y > box.Bottom() {
			continue
		}
		l.textIn(ln.Text, body, fg, tree.Rect{X: box.X, Y: y, W: box.W, H: lh}, AlignStart, textClip)
	}
	// Placeholder text while the empty field has focus; an unfocused empty
	// field above already shows its label instead.
	if c.Focused && c.Text.Get() == "" && c.Preedit == "" && c.Placeholder.Get() != "" {
		l.textIn(c.Placeholder.Get(), body, hint,
			tree.Rect{X: box.X, Y: box.Y, W: box.W, H: lh}, AlignStart, textClip)
	}
	advance := func(s string) float64 { return textWidth(l, s, body) }
	if c.Focused && c.Preedit != "" {
		idx, x := areaCaret(lines, c.Caret, advance)
		y := box.Y + float64(idx)*lh - shift
		l.textIn(c.Preedit, body, fg, tree.Rect{X: box.X + x, Y: y, W: box.W - x, H: lh}, AlignStart, textClip)
	}
	if c.Focused {
		idx, x := areaCaret(lines, c.Caret, advance)
		y := box.Y + float64(idx)*lh - shift
		if y+lh > box.Y && y < box.Bottom() {
			l.Ops = append(l.Ops, Op{
				Kind: Fill, Shape: RoundedRect,
				Rect:  [4]float32{float32(box.X + x), float32(y + 3), 2, float32(lh - 6)},
				Color: col.Primary, Clip: clipArr(textClip),
			})
		}
	}
	if c.Supporting.Get() != "" {
		l.textIn(c.Supporting.Get(), small, hint,
			tree.Rect{X: r.X + textAreaPad, Y: r.Bottom() + 4, W: r.W, H: 16}, AlignStart, clip)
	}
}

// areaSelection draws the highlight for a byte range, one rectangle per line it
// touches.
func (l *List) areaSelection(lines []text.Line, src string, a, b int, box tree.Rect, st m3.TextStyle, lh, shift float64, clip tree.Rect) {
	if a > b {
		a, b = b, a
	}
	for i, ln := range lines {
		if ln.End < a || ln.Start > b {
			continue
		}
		from := clampInt(a-ln.Start, 0, len(ln.Text))
		to := clampInt(b-ln.Start, 0, len(ln.Text))
		if from == to {
			continue
		}
		x0 := textWidth(l, ln.Text[:from], st)
		x1 := textWidth(l, ln.Text[:to], st)
		if x1 < x0 {
			x0, x1 = x1, x0
		}
		if x1-x0 == 0 {
			x1 = x0 + 1
		}
		y := box.Y + float64(i)*lh - shift
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect,
			Rect:   [4]float32{float32(box.X + x0), float32(y + lh*0.1), float32(x1 - x0), float32(lh * 0.8)},
			Radius: [4]float32{2, 2, 2, 2},
			Color:  withAlpha(l.Theme.Colors.Primary, m3.SelectionOpacity),
			Clip:   clipArr(clip),
		})
	}
}

// areaCaret returns the visual line the caret is on and the x offset of the
// caret inside that line.
func areaCaret(lines []text.Line, caret int, advance func(string) float64) (int, float64) {
	if len(lines) == 0 {
		return 0, 0
	}
	for i, ln := range lines {
		// A caret at a line end belongs to that line, not the next one.
		if caret >= ln.Start && caret <= ln.End {
			return i, advance(ln.Text[:caret-ln.Start])
		}
	}
	return len(lines) - 1, 0
}

// areaScroll is the vertical shift that keeps the caret line inside the box.
func areaScroll(c *tree.Component, lines []text.Line, box tree.Rect, lh float64) float64 {
	line := 0
	for i, ln := range lines {
		if c.Caret >= ln.Start && c.Caret <= ln.End {
			line = i
			break
		}
	}
	visible := int(box.H / lh)
	if visible < 1 {
		visible = 1
	}
	shift := c.TextScroll
	if line < int(shift/lh) {
		shift = float64(line) * lh
	}
	if line >= int(shift/lh)+visible {
		shift = float64(line-visible+1) * lh
	}
	return maxf(shift, 0)
}
