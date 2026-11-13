package ui

import "github.com/goi-gui/goi/internal/tree"

// syncMarkdown rebuilds every Markdown whose source changed. A design's text is
// parsed when the tree is built, but Go changes it later with SetSource or
// SetData, and the children have to exist before layout measures them, so this
// runs with the other sync passes.
func (a *App) syncMarkdown() bool {
	if a.win == nil || a.win.tree == nil {
		return false
	}
	changed := false
	a.win.tree.Walk(func(n tree.Node) {
		if md, ok := n.(*tree.Markdown); ok && md.Rebuild() {
			changed = true
		}
	})
	return changed
}
