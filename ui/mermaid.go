package ui

import (
	"github.com/goi-gui/goi/internal/mermaid"
	"github.com/goi-gui/goi/internal/tree"
)

// syncMermaid renders every visible Mermaid whose source or scheme changed.
// Rendering is in the ui layer because it needs the theme and a rasterizer; the
// tree stays free of both and only holds the bitmap layout measures.
func (a *App) syncMermaid() bool {
	if a.win == nil || a.win.tree == nil {
		return false
	}
	theme := themeFor(a.win)
	changed := false
	a.win.tree.Walk(func(n tree.Node) {
		m, ok := n.(*tree.Mermaid)
		if !ok || !nodeVisible(n) {
			return
		}
		src := m.Text.Get()
		key := "text:" + src
		if src == "" && m.Src.Get() != "" {
			key = "src:" + m.Src.Get()
			b, err := a.readSrc(m.Src.Get())
			if err != nil {
				m.RenderedText, m.RenderedTheme = key, theme.Name
				m.Data = nil
				m.IntrinsicW, m.IntrinsicH = 0, 0
				m.LoadErr = err
				m.Base().MarkDirty(tree.DirtyLayout)
				changed = true
				return
			}
			src = string(b)
		}
		if m.RenderedText == key && m.RenderedTheme == theme.Name {
			return
		}
		m.RenderedText, m.RenderedTheme = key, theme.Name
		m.LoadErr = nil
		if src == "" {
			m.Data = nil
			m.IntrinsicW, m.IntrinsicH = 0, 0
		} else {
			engine, err := a.mermaidEngine()
			if err == nil {
				m.Data, m.IntrinsicW, m.IntrinsicH, err = engine.Render(src, theme)
			}
			if err != nil {
				m.Data = nil
				m.IntrinsicW, m.IntrinsicH = 0, 0
				m.LoadErr = err
			}
		}
		m.Base().MarkDirty(tree.DirtyLayout)
		changed = true
	})
	return changed
}

// mermaidEngine creates the renderer once. A failed start is remembered: the
// next frame should draw the error, not compile resvg again.
func (a *App) mermaidEngine() (*mermaid.Engine, error) {
	if a.mermaidEng != nil {
		return a.mermaidEng, nil
	}
	if a.mermaidErr != nil {
		return nil, a.mermaidErr
	}
	engine, err := mermaid.NewEngine()
	if err != nil {
		a.mermaidErr = err
		return nil, err
	}
	a.mermaidEng = engine
	return engine, nil
}

// nodeVisible reports whether n and every ancestor is visible, so a diagram on
// a hidden gallery page is not rendered before the page opens.
func nodeVisible(n tree.Node) bool {
	for ; n != nil; n = n.Base().Parent() {
		if !n.Base().Visible() {
			return false
		}
	}
	return true
}
