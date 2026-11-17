//go:build !js && !nomermaid

// Package mermaid renders Mermaid source with Pele's layout and resvg, both
// running in-process: no browser, no Node and no external process.
package mermaid

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
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
//
// targetW and targetH are the logical box the bitmap is going to be drawn in.
// The raster scale follows the contained size, at two device pixels per logical
// pixel, so a small diagram scaled up by the design stays sharp instead of
// becoming a blurry enlargement, and a tall diagram in a short box does not
// allocate pixels it will never show. Zero keeps the fixed 2x.
func (e *Engine) Render(src string, theme *m3.Theme, targetW, targetH float64) (*img.Data, float64, float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
	defer cancel()
	res, err := e.renderer.Render(ctx, src, raster.Options{MaxWidth: maxWidth})
	if err != nil {
		return nil, 0, 0, err
	}
	scale := 2.0
	if targetW > 0 && res.Width > 0 {
		fit := targetW / res.Width
		if targetH > 0 && res.Height > 0 && targetH/res.Height < fit {
			fit = targetH / res.Height
		}
		scale = fit * 2
		if scale < 1 {
			scale = 1
		}
		if scale > 4 {
			scale = 4
		}
	}
	im, err := e.raster.Rasterize(res, palette(theme), raster.RasterOptions{
		Scale:   scale,
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
	// The raster is transparent around the drawing, which leaves dark labels on
	// a dark page and changes the picture with whatever sits behind it. Compose
	// it onto the scheme's surface so the diagram carries its own background.
	bg := m3.Light().Colors.Surface
	if theme != nil {
		bg = theme.Colors.Surface
	}
	out := image.NewRGBA(b)
	draw.Draw(out, b, &image.Uniform{C: rgb(bg)}, image.Point{}, draw.Src)
	draw.Draw(out, b, im, b.Min, draw.Over)
	return &img.Data{W: b.Dx(), H: b.Dy(), Pix: out.Pix}, w, h, nil
}

// rgb turns an 0xRRGGBBAA role into an image colour.
func rgb(c uint32) color.RGBA {
	return color.RGBA{R: uint8(c >> 24), G: uint8(c >> 16), B: uint8(c >> 8), A: uint8(c)}
}

// palette maps Pele's variables to the current scheme: surfaces and lines match
// a card, text matches body text, and the accent is the primary role.
func palette(theme *m3.Theme) raster.Theme {
	if theme == nil {
		return nil
	}
	c := theme.Colors
	th := raster.Theme{
		"pele-bg":          hex(c.Surface),
		"pele-fg":          hex(c.OnSurface),
		"pele-muted":       hex(c.OnSurfaceVariant),
		"pele-line":        hex(c.Outline),
		"pele-surface":     hex(c.SurfaceContainerHigh),
		"pele-surface-alt": hex(c.SurfaceContainerHighest),
		"pele-border":      hex(c.Outline),
		"pele-accent":      hex(c.Primary),
		"pele-radius":      "4px",
	}
	if theme.Dark {
		// Labels on a coloured shape use the background colour for contrast; in
		// a dark scheme that is nearly black, so the series have to be light
		// enough to carry it. The light scheme keeps Pele's own palette, where
		// those labels are white on a medium series colour.
		light := []string{"#a8c7fa", "#f7d08a", "#a5d6a7", "#f4a6a6",
			"#a8d8d4", "#f2e08a", "#d6b3d0", "#cbb3a4"}
		for i, v := range light {
			th[fmt.Sprintf("pele-series-%d", i+1)] = v
		}
	}
	return th
}

// hex formats a theme colour as the #rrggbb the SVG expects.
func hex(c uint32) string { return fmt.Sprintf("#%06x", c>>8) }
