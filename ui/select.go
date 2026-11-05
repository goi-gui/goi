package ui

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
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

// selectableAt returns the nearest node whose text a reader can select: a Text
// with selectable: true, or a Code block, which always allows it.
func selectableAt(hit tree.Node) tree.Node {
	for n := hit; n != nil; n = n.Base().Parent() {
		switch v := n.(type) {
		case *tree.Text:
			if v.Selectable.Get() {
				return v
			}
		case *tree.Code:
			return v
		}
	}
	return nil
}

// codeCopyAt returns the Code block whose copy button holds the point, if any.
func codeCopyAt(hit tree.Node, x, y float64) *tree.Code {
	for n := hit; n != nil; n = n.Base().Parent() {
		code, ok := n.(*tree.Code)
		if !ok {
			continue
		}
		if !code.ShowCopy.Get() {
			return nil
		}
		if hitRect(paint.CodeCopyRect(absoluteRect(code)), x, y) {
			return code
		}
		return nil
	}
	return nil
}

// selText is the text a selectable node draws, which is what a byte offset in
// its selection indexes.
func selText(n tree.Node) string {
	switch v := n.(type) {
	case *tree.Text:
		return v.Text.Get()
	case *tree.Code:
		return v.Source()
	}
	return ""
}

// selRange returns a node's selection in byte offsets.
func selRange(n tree.Node) (int, int) {
	switch v := n.(type) {
	case *tree.Text:
		return v.SelA, v.SelB
	case *tree.Code:
		return v.SelA, v.SelB
	}
	return 0, 0
}

// setSel replaces a node's selection and repaints it.
func setSel(n tree.Node, a, b int) {
	switch v := n.(type) {
	case *tree.Text:
		v.SelA, v.SelB = a, b
	case *tree.Code:
		v.SelA, v.SelB = a, b
	default:
		return
	}
	n.Base().MarkDirty(tree.DirtyPaint)
}

// selIndexAt turns a pointer position into a byte offset in a selectable node.
func (i *interactor) selIndexAt(n tree.Node, x, y float64) int {
	if code, ok := n.(*tree.Code); ok {
		return i.codeIndexAt(code, x, y)
	}
	t, ok := n.(*tree.Text)
	if !ok {
		return 0
	}
	return i.textIndexAt(t, x, y)
}

// codeIndexAt maps a pointer to a byte offset inside a Code block. Its lines are
// hard breaks, so only the line and the advance within it matter.
func (i *interactor) codeIndexAt(code *tree.Code, x, y float64) int {
	r := absoluteRect(code)
	box := paint.CodeBox(r)
	st := paint.CodeStyle()
	lines := paint.CodeLines(code.Source())
	if len(lines) == 0 {
		return 0
	}
	li := int((y - box.Y) / st.LineHeight)
	if li < 0 {
		li = 0
	}
	if li >= len(lines) {
		li = len(lines) - 1
	}
	line := lines[li]
	rel := x - box.X
	if rel <= 0 {
		return line.Start
	}
	advance := func(s string) float64 { return i.measureStyled(s, st) }
	best, bestD := line.Start, rel
	for idx := range line.Text {
		if idx == 0 {
			continue
		}
		if d := abs(advance(line.Text[:idx]) - rel); d < bestD {
			best, bestD = line.Start+idx, d
		}
	}
	if d := abs(advance(line.Text) - rel); d < bestD {
		best = line.End
	}
	return best
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
	n := i.textSel
	if n == nil {
		return
	}
	i.textSel = nil
	a, b := selRange(n)
	if a == b {
		return
	}
	setSel(n, 0, 0)
	i.markDirty()
}

// copyTextSelection puts a selectable node's selection on the clipboard and
// reports whether there was one.
func (i *interactor) copyTextSelection() bool {
	n := i.textSel
	if n == nil {
		return false
	}
	a, b := selRange(n)
	if a > b {
		a, b = b, a
	}
	src := selText(n)
	if a == b || b > len(src) {
		return false
	}
	i.putClipboard(src[a:b])
	return true
}
