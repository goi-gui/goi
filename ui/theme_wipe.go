package ui

import (
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

// buildWipeList paints the new scheme above the moving boundary and the old one
// below it. The tree is resolved twice, because role colours live in cells; the
// second pass leaves the new scheme in place.
func (a *App) buildWipeList(l *paint.List, tr *tree.Tree, w, h float64) {
	if tr == nil || tr.Root == nil {
		return
	}
	p := a.themeWipeP
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	y := h * p
	top := tree.Rect{W: w, H: y}
	bottom := tree.Rect{Y: y, W: w, H: h - y}
	oldTheme, newTheme := a.themeFrom, a.themeTo
	if oldTheme == nil {
		oldTheme = newTheme
	}
	if oldTheme == nil || newTheme == nil {
		return
	}

	a.resolveColorsFor(oldTheme)
	l.Theme = oldTheme
	l.BuildClip(tr, w, h, bottom)
	oldOps := append([]paint.Op(nil), l.Ops...)
	oldAnimating := l.Animating

	a.resolveColorsFor(newTheme)
	l.Theme = newTheme
	l.BuildClip(tr, w, h, top)

	oldFill := paint.Op{
		Kind: paint.Fill, Shape: paint.RoundedRect,
		Rect:  wipeRect(tree.Rect{W: w, H: h}),
		Color: oldTheme.Colors.Surface, Clip: wipeRect(bottom),
	}
	newFill := paint.Op{
		Kind: paint.Fill, Shape: paint.RoundedRect,
		Rect:  wipeRect(tree.Rect{W: w, H: h}),
		Color: newTheme.Colors.Surface, Clip: wipeRect(top),
	}
	newOps := append([]paint.Op{newFill}, l.Ops...)
	l.Ops = append(append([]paint.Op{oldFill}, oldOps...), newOps...)
	l.Animating = oldAnimating || l.Animating
	l.Theme = newTheme
}

func wipeRect(r tree.Rect) [4]float32 {
	return [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)}
}
