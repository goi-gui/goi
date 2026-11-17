package paint

import (
	"github.com/goi-gui/goi/internal/tree"
)

// paintMermaid draws the bitmap the ui layer rendered from the diagram. The
// bitmap keeps its aspect inside the box, so a design that sets only a width
// still gets the diagram's own height from layout.
func (l *List) paintMermaid(c *tree.Component, r tree.Rect, clip tree.Rect) {
	l.paintBox(c.Base(), r, clip)
	m, ok := c.Node().(*tree.Mermaid)
	if !ok {
		return
	}
	if m.Data != nil {
		l.paintBitmap(m.Data, "contain", &m.NodeBase, m.Loading, 0, r, clip)
		return
	}
	if m.LoadErr != nil {
		l.textIn("Mermaid: "+m.LoadErr.Error(), l.style("bodySmall"), l.Theme.Colors.Error,
			tree.Rect{X: r.X + 8, Y: r.Y + 8, W: maxf(r.W-16, 0), H: maxf(r.H-16, 0)},
			AlignStart, clip)
		return
	}
	if m.Loading {
		// A faint wash marks the box while the worker compiles the renderer or
		// lays the diagram out; the frame loop is free to keep painting.
		l.paintBitmap(nil, "contain", &m.NodeBase, true, 0, r, clip)
	}
}
