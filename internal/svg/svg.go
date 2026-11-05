// Package svg parses SVG files and rasterizes them into the renderer's pixel
// format with currentColor replaced by the colour the design chose.
package svg

import (
	"bytes"
	"fmt"
	"image"
	"math"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"

	"github.com/goi-gui/goi/internal/img"
)

// Rasterizing at a multiple of the viewBox keeps an icon sharp when the desktop
// scales it up, without allocating a huge bitmap for a large viewBox.
const (
	rasterScale   = 4.0
	minRasterEdge = 64
	maxRasterEdge = 1024
)

// SVG is one parsed file. The source is kept so a colour change can be applied
// again without reading the file.
type SVG struct {
	src []byte
	w   float64
	h   float64
}

// Parse reads an SVG and returns its viewBox size. currentColor is replaced
// with black for the parse, so a file that uses it still parses.
func Parse(b []byte) (*SVG, error) {
	icon, err := oksvg.ReadReplacingCurrentColor(bytes.NewReader(b), "#000000")
	if err != nil {
		return nil, fmt.Errorf("svg: parse: %w", err)
	}
	w, h := icon.ViewBox.W, icon.ViewBox.H
	if w <= 0 || h <= 0 {
		// A file with neither a viewBox nor width/height has no intrinsic size;
		// a small square gives it somewhere to draw.
		w, h = 24, 24
	}
	return &SVG{src: b, w: w, h: h}, nil
}

// Intrinsic is the viewBox size in logical pixels.
func (s *SVG) Intrinsic() (w, h float64) { return s.w, s.h }

// Render rasterizes the file with color as currentColor. The alpha channel of
// color becomes the drawing opacity, so a half-transparent design colour keeps
// working even though SVG hex colours have no alpha.
func (s *SVG) Render(color uint32) (*img.Data, error) {
	icon, err := oksvg.ReadReplacingCurrentColor(bytes.NewReader(s.src), hexRGB(color))
	if err != nil {
		return nil, fmt.Errorf("svg: render: %w", err)
	}
	if icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		icon.ViewBox.W, icon.ViewBox.H = s.w, s.h
	}
	w, h := s.pixels()
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, m, m.Bounds())
	raster := rasterx.NewDasher(w, h, scanner)
	icon.SetTarget(0, 0, float64(w), float64(h))
	icon.Draw(raster, float64(color&0xFF)/255)
	return &img.Data{W: w, H: h, Pix: m.Pix}, nil
}

// pixels is the raster size: the viewBox scaled up, within the cap.
func (s *SVG) pixels() (int, int) {
	longest := math.Max(s.w, s.h)
	scale := rasterScale
	if longest*scale < minRasterEdge {
		scale = minRasterEdge / longest
	}
	if longest*scale > maxRasterEdge {
		scale = maxRasterEdge / longest
	}
	return max(1, int(math.Round(s.w*scale))), max(1, int(math.Round(s.h*scale)))
}

// hexRGB formats the RGB half of 0xRRGGBBAA the way oksvg expects.
func hexRGB(c uint32) string { return fmt.Sprintf("#%06x", c>>8) }
