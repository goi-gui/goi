package ui

import (
	"fmt"
	"math"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/mermaid"
	"github.com/goi-gui/goi/internal/tree"
)

// mermaidCacheBytes is how many decoded diagrams the app keeps for pages that
// are opened again. Diagrams are bitmaps, so the cap is in pixels, not entries.
const mermaidCacheBytes = 64 << 20

type mermaidBitmap struct {
	data *img.Data
	w, h float64
}

// syncMermaid starts a render for every visible diagram whose source or scheme
// changed and applies the cached result when there is one. The walk runs before
// layout, so a diagram that scrolls in is picked up on the first frame that
// draws it.
func (a *App) syncMermaid() bool {
	if a.win == nil || a.win.tree == nil {
		return false
	}
	vw, vh := a.win.tree.Viewport()
	if vw <= 0 || vh <= 0 {
		a.viewportWait = true
		return false
	}
	theme := themeFor(a.win)
	themeKey := mermaid.ThemeKey(theme)
	changed := false
	a.win.tree.Walk(func(n tree.Node) {
		m, ok := n.(*tree.Mermaid)
		if !ok {
			return
		}
		if !a.nodeOnScreen(n) {
			// The bitmap is the large part. Keep the intrinsic size so the page
			// does not jump, but let the pixels go; scrolling back takes them
			// from the cache or renders again.
			if m.Data != nil {
				m.Data = nil
				m.RenderedKey = ""
				m.Base().MarkDirty(tree.DirtyPaint)
				changed = true
			}
			return
		}
		src := m.Source.Get()
		// The key carries a coarse box width: a resize re-renders at a scale
		// that keeps the diagram sharp, while a few pixels of layout jitter do
		// not start a new render.
		rect := absoluteRect(m)
		boxW, boxH := rect.W, rect.H
		widthKey := int(math.Round(boxW/64)) * 64
		heightKey := int(math.Round(boxH/64)) * 64
		key := themeKey + "\x00" + fmt.Sprint(widthKey) + "x" + fmt.Sprint(heightKey) + "\x00" + src
		if src == "" {
			if m.RenderedKey != key || m.Data != nil || m.Loading {
				m.RenderedKey, m.PendingKey = key, ""
				m.Data, m.IntrinsicW, m.IntrinsicH = nil, 0, 0
				m.Loading, m.LoadErr = false, nil
				m.Base().MarkDirty(tree.DirtyLayout)
				changed = true
			}
			return
		}
		if m.RenderedKey == key && !m.Loading {
			return
		}
		if m.PendingKey == key {
			return
		}
		if bmp, ok := a.mermaidCache[key]; ok {
			applyMermaid(m, key, bmp)
			changed = true
			return
		}
		m.PendingKey, m.Loading, m.LoadErr = key, true, nil
		if a.mermaidPending == nil {
			a.mermaidPending = map[string]bool{}
		}
		if !a.mermaidPending[key] {
			a.mermaidPending[key] = true
			a.renderMermaidAsync(key, src, theme, boxW, boxH)
		}
		m.Base().MarkDirty(tree.DirtyPaint)
		changed = true
	})
	return changed
}

func applyMermaid(m *tree.Mermaid, key string, b mermaidBitmap) {
	m.Data, m.IntrinsicW, m.IntrinsicH = b.data, b.w, b.h
	m.RenderedKey, m.PendingKey = key, ""
	m.Loading, m.LoadErr = false, nil
	m.Base().MarkDirty(tree.DirtyLayout)
}

// renderMermaidAsync runs one layout and raster off the UI goroutine. The
// engine and its wasm rasterizer are serialised by mermaidMu, because the
// rasterizer is not safe for concurrent use. The first call also compiles resvg,
// which is what used to freeze a window when the first diagram appeared.
func (a *App) renderMermaidAsync(key, src string, theme *m3.Theme, targetW, targetH float64) {
	go func() {
		a.mermaidMu.Lock()
		defer a.mermaidMu.Unlock()
		if a.mermaidClosed {
			return
		}
		engine, err := a.mermaidEngineLocked()
		var data *img.Data
		var w, h float64
		if err == nil {
			data, w, h, err = engine.Render(src, theme, targetW, targetH)
		}
		a.Post(func() { a.finishMermaid(key, data, w, h, err) })
	}()
}

func (a *App) mermaidEngineLocked() (*mermaid.Engine, error) {
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

// finishMermaid applies a finished render to every node waiting for that key.
func (a *App) finishMermaid(key string, data *img.Data, w, h float64, err error) {
	delete(a.mermaidPending, key)
	if err == nil && data != nil {
		a.cacheMermaid(key, mermaidBitmap{data: data, w: w, h: h})
	}
	if a.win == nil || a.win.tree == nil {
		return
	}
	changed := false
	a.win.tree.Walk(func(n tree.Node) {
		m, ok := n.(*tree.Mermaid)
		if !ok || m.PendingKey != key {
			return
		}
		m.PendingKey = ""
		m.Loading = false
		m.RenderedKey = key
		m.LoadErr = err
		if err != nil || data == nil {
			m.Data, m.IntrinsicW, m.IntrinsicH = nil, 0, 0
		} else {
			m.Data, m.IntrinsicW, m.IntrinsicH = data, w, h
		}
		m.Base().MarkDirty(tree.DirtyLayout)
		changed = true
	})
	if changed {
		a.needsFrame = true
	}
}

// cacheMermaid stores a finished bitmap, evicting the oldest diagrams when the
// pixel budget is full. A node keeps its own pointer even if the cache drops it.
func (a *App) cacheMermaid(key string, b mermaidBitmap) {
	if a.mermaidCache == nil {
		a.mermaidCache = map[string]mermaidBitmap{}
	}
	if _, ok := a.mermaidCache[key]; ok {
		return
	}
	a.mermaidCache[key] = b
	a.mermaidOrder = append(a.mermaidOrder, key)
	a.mermaidBytes += len(b.data.Pix)
	for a.mermaidBytes > mermaidCacheBytes && len(a.mermaidOrder) > 1 {
		old := a.mermaidOrder[0]
		a.mermaidOrder = a.mermaidOrder[1:]
		if ob, ok := a.mermaidCache[old]; ok {
			a.mermaidBytes -= len(ob.data.Pix)
			delete(a.mermaidCache, old)
		}
	}
}

// closeMermaid stops starting renders and frees the engine. The lock waits for
// a render already in flight, so the rasterizer is not closed under it.
func (a *App) closeMermaid() {
	a.mermaidMu.Lock()
	a.mermaidClosed = true
	if a.mermaidEng != nil {
		_ = a.mermaidEng.Close()
		a.mermaidEng = nil
	}
	a.mermaidMu.Unlock()
}
