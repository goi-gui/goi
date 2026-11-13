package ui

import (
	"github.com/goi-gui/goi/internal/tree"
)

// linkAt returns the link under a point: a RichText run's, or a Text's own.
// A press that lands on one is a link press, not a paragraph press.
func (i *interactor) linkAt(n tree.Node, x, y float64) (string, bool) {
	switch v := n.(type) {
	case *tree.RichText:
		return i.richLinkAt(v, x, y)
	case *tree.Text:
		if link := v.Link.Get(); link != "" {
			return link, true
		}
	}
	return "", false
}

// richLinkAt returns the link of the run under a point. The paragraph is
// wrapped with the same function and the same advances the painter used, so the
// box a click lands in is the box that was drawn.
func (i *interactor) richLinkAt(rt *tree.RichText, x, y float64) (string, bool) {
	if !rt.HasLink() {
		return "", false
	}
	r := absoluteRect(rt)
	st := textStyle(&rt.Text)
	lh := st.LineHeight
	if lh <= 0 {
		lh = st.Size * 1.2
	}
	lines := rt.Lines(r.W, st.Weight, func(weight int, s string) float64 {
		m := st
		m.Weight = weight
		return i.measureStyled(s, m)
	})
	if len(lines) == 0 {
		return "", false
	}
	li := int((y - r.Y) / lh)
	if li < 0 {
		li = 0
	}
	if li >= len(lines) {
		li = len(lines) - 1
	}
	line := lines[li]
	if len(line) == 0 {
		return "", false
	}
	lineW := line[len(line)-1].X + line[len(line)-1].W
	off := 0.0
	switch rt.Align.Get() {
	case "center":
		off = (r.W - lineW) / 2
	case "end":
		off = r.W - lineW
	}
	spans := rt.Spans()
	for _, run := range line {
		if run.Style < 0 || run.Style >= len(spans) {
			continue
		}
		bx := r.X + off + run.X
		if x >= bx && x <= bx+run.W && spans[run.Style].Link != "" {
			return spans[run.Style].Link, true
		}
	}
	return "", false
}

// clickLink runs the click on a link: the app's handler when there is one,
// otherwise the desktop. A RichText's handler slot is the paragraph's, so every
// run of it lands in the same place.
func (i *interactor) clickLink(n tree.Node, link string) {
	if t := textOf(n); t != nil && t.LinkClick.Len() > 0 {
		t.LinkClick.Emit(link)
		return
	}
	i.openLink(link)
}

// textOf returns the Text behind a node: a RichText's runs are spans, so the
// paragraph itself holds the handler slot.
func textOf(n tree.Node) *tree.Text {
	switch v := n.(type) {
	case *tree.RichText:
		return &v.Text
	case *tree.Text:
		return v
	}
	return nil
}

// openLink hands a clicked link to the desktop. An interactor without an opener
// — a headless test — opens nothing.
func (i *interactor) openLink(link string) {
	if i.open == nil {
		return
	}
	if err := i.open(link); err != nil && i.app != nil {
		i.app.reportError(err)
	}
}
