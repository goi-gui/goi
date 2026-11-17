package ui

import "github.com/goi-gui/goi/internal/tree"

// lazyNode reports whether an element defers its content until it is on screen.
func lazyNode(n tree.Node) bool {
	switch x := n.(type) {
	case *tree.Image:
		return x.Lazy.Get()
	case *tree.SVG:
		return x.Lazy.Get()
	case *tree.Audio:
		return x.Media.Lazy.Get()
	case *tree.Video:
		return x.Media.Lazy.Get()
	}
	return false
}

// lazyDeferred reports whether a lazy element should still wait. The box is
// laid out first, so this only asks whether any part of it is inside the window
// and the clipping ancestors' frames. A zero-sized box is not deferred: its
// content is what gives it a size (an Image or SVG with no declared width and
// height), and waiting would never end.
func (a *App) lazyDeferred(n tree.Node) bool {
	if !lazyNode(n) {
		return false
	}
	if a.win == nil || a.win.tree == nil {
		return false
	}
	if !n.Base().Visible() {
		return true
	}
	vw, vh := a.win.tree.Viewport()
	if vw <= 0 || vh <= 0 {
		return true // the first layout has not placed anything yet
	}
	r := absoluteRect(n)
	if r.W <= 0 || r.H <= 0 {
		return false
	}
	clip := tree.Rect{W: vw, H: vh}
	var chain []tree.Node
	for p := n.Base().Parent(); p != nil; p = p.Base().Parent() {
		if !p.Base().Visible() {
			return true
		}
		chain = append(chain, p)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		p := chain[i]
		if p.Base().ClipsChildren() {
			clip = intersectRect(clip, p.Base().ScrollClip(absoluteRect(p)))
		}
	}
	vis := intersectRect(r, clip)
	return vis.W <= 0 || vis.H <= 0
}
