package ui

import (
	"github.com/ksckaan1/goui/internal/m3"
	"github.com/ksckaan1/goui/internal/text"
	"github.com/ksckaan1/goui/internal/tree"
)

// textStyle resolves a Text node's style the same way the painter does, so
// selection geometry and drawn glyphs agree.
func textStyle(t *tree.Text) m3.TextStyle {
	st := m3.RoleStyle(t.Role.Get())
	if v := t.FontSize.Get(); v > 0 {
		st.Size = v
		st.LineHeight = v * 1.2
	}
	if v := t.Weight.Get(); v > 0 {
		st.Weight = int(v)
	}
	return st
}

// selectableTextAt returns the nearest Text with selectable: true.
func selectableTextAt(hit tree.Node) *tree.Text {
	for n := hit; n != nil; n = n.Base().Parent() {
		if t, ok := n.(*tree.Text); ok && t.Selectable.Get() {
			return t
		}
	}
	return nil
}

// textLines returns the byte ranges of the lines a selectable Text draws.
func (i *interactor) textLines(t *tree.Text, width float64) []text.Line {
	st := textStyle(t)
	if !t.Wrap.Get() || width <= 0 {
		s := t.Text.Get()
		return []text.Line{{Text: s, Start: 0, End: len(s)}}
	}
	return text.WrapRanges(t.Text.Get(), width, func(run string) float64 {
		return i.measureStyled(run, st)
	})
}

// textIndexAt turns a pointer position into a byte offset in the source text.
func (i *interactor) textIndexAt(t *tree.Text, x, y float64) int {
	// The pointer is in window space, so the box must include the scroll
	// offsets of every ancestor; absoluteRect does that.
	r := absoluteRect(t)
	st := textStyle(t)
	lh := st.LineHeight
	if lh <= 0 {
		lh = st.Size * 1.2
	}
	lines := i.textLines(t, r.W)
	if len(lines) == 0 {
		return 0
	}
	li := int((y - r.Y) / lh)
	if li < 0 {
		li = 0
	}
	if li >= len(lines) {
		li = len(lines) - 1
	}
	line := lines[li]
	lineW := i.measureStyled(line.Text, st)
	off := 0.0
	switch t.Align.Get() {
	case "center":
		off = (r.W - lineW) / 2
	case "end":
		off = r.W - lineW
	}
	rel := x - (r.X + off)
	if rel <= 0 {
		return line.Start
	}
	best, bestD := line.Start, rel
	for idx := range line.Text {
		if idx == 0 {
			continue
		}
		if d := abs(i.measureStyled(line.Text[:idx], st) - rel); d < bestD {
			best, bestD = line.Start+idx, d
		}
	}
	if d := abs(lineW - rel); d < bestD {
		best = line.End
	}
	return best
}

func (i *interactor) measureStyled(s string, st m3.TextStyle) float64 {
	if s == "" {
		return 0
	}
	if i.measure != nil {
		return i.measure(s, st.Size, st.Weight)
	}
	return float64(len([]rune(s))) * st.Size * 0.55
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// clearTextSelection drops the static-text selection and repaints the node.
func (i *interactor) clearTextSelection() {
	t := i.textSel
	if t == nil {
		return
	}
	i.textSel = nil
	if t.SelA == t.SelB {
		return
	}
	t.SelA, t.SelB = 0, 0
	t.Base().MarkDirty(tree.DirtyPaint)
	i.markDirty()
}

// copyTextSelection puts a selectable Text's selection on the clipboard and
// reports whether there was one.
func (i *interactor) copyTextSelection() bool {
	t := i.textSel
	if t == nil {
		return false
	}
	a, b := t.SelA, t.SelB
	if a > b {
		a, b = b, a
	}
	if a == b {
		return false
	}
	i.putClipboard(t.Text.Get()[a:b])
	return true
}
