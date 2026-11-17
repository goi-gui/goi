//go:build !js

// Package mermaid renders Mermaid source with Pele's layout and resvg, both
// running in-process: no browser, no Node and no external process.
package mermaid

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Broderick-Westrope/pele-go/raster"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/m3"
)

// maxWidth keeps a very wide diagram from laying itself out across a page.
const maxWidth = 1600

// renderTimeout bounds one layout and one raster, so a pathological source
// cannot stall a render forever.
const renderTimeout = 5 * time.Second

// Engine owns the two halves of rendering. The layout renderer is safe for
// concurrent use; the rasterizer is not, so the ui layer calls one engine from
// one goroutine.
type Engine struct {
	renderer *raster.Renderer
	raster   *raster.Rasterizer
}

// NewEngine prepares the layout and rasterizer. The first one compiles resvg's
// wasm unless a cacheDir has a copy; the ui layer calls this off its own
// goroutine so the first diagram does not hold up a frame.
func NewEngine() (*Engine, error) {
	r, err := raster.NewRenderer()
	if err != nil {
		return nil, fmt.Errorf("mermaid: %w", err)
	}
	cache := ""
	if dir, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(dir, "goi", "mermaid")
		if os.MkdirAll(dir, 0o755) == nil {
			cache = dir
		}
	}
	rast, err := raster.NewRasterizer(context.Background(), cache)
	if err != nil {
		return nil, fmt.Errorf("mermaid: %w", err)
	}
	return &Engine{renderer: r, raster: rast}, nil
}

// Close releases the rasterizer's wasm instance.
func (e *Engine) Close() error {
	if e == nil || e.raster == nil {
		return nil
	}
	return e.raster.Close()
}

// Render turns Mermaid source into a bitmap and its logical size. The theme
// colours replace Pele's palette, so a diagram sits on the same surfaces and
// lines as the rest of the app.
func (e *Engine) Render(src string, theme *m3.Theme) (*img.Data, float64, float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
	defer cancel()
	res, err := e.renderer.Render(ctx, src, raster.Options{MaxWidth: maxWidth})
	if err != nil {
		return nil, 0, 0, err
	}
	im, err := e.raster.Rasterize(res, palette(theme), raster.RasterOptions{
		Scale:   2,
		Timeout: renderTimeout,
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("mermaid: %w", err)
	}
	w, h := res.Width, res.Height
	if w <= 0 || h <= 0 {
		return nil, 0, 0, fmt.Errorf("mermaid: the diagram has no size")
	}
	b := im.Bounds()
	return &img.Data{W: b.Dx(), H: b.Dy(), Pix: im.Pix}, w, h, nil
}

// palette maps Pele's variables to the current scheme: surfaces and lines match
// a card, text matches body text, and the accent is the primary role.
func palette(theme *m3.Theme) raster.Theme {
	if theme == nil {
		return nil
	}
	c := theme.Colors
	return raster.Theme{
		"pele-bg":          hex(c.Surface),
		"pele-fg":          hex(c.OnSurface),
		"pele-muted":       hex(c.OnSurfaceVariant),
		"pele-line":        hex(c.Outline),
		"pele-surface":     hex(c.SurfaceContainerHigh),
		"pele-surface-alt": hex(c.SurfaceContainerHighest),
		"pele-border":      hex(c.OutlineVariant),
		"pele-accent":      hex(c.Primary),
		"pele-radius":      "4px",
	}
}

// hex formats a theme colour as the #rrggbb the SVG expects.
func hex(c uint32) string { return fmt.Sprintf("#%06x", c>>8) }
