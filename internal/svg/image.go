//go:build all || svg

package svg

import (
	"encoding/base64"
	"image"
	"os"
	"strings"

	"github.com/goi-gui/goi/internal/img"
)

// drawImages paints embedded raster <image> elements into a layer behind the
// vector content. A data URI is the form an SVG carries when it has to be
// self-contained; a relative file path has no base directory here and is left
// to the app's own asset loading.
func drawImages(dst *image.RGBA, root *elem, scale float64) bool {
	if root == nil {
		return false
	}
	rootM := affine{scale, 0, 0, scale, 0, 0}
	drawn := false
	var walk func(e *elem, m affine, hidden bool)
	walk = func(e *elem, m affine, hidden bool) {
		if hidden || e.name.Local == "defs" || e.name.Local == "clipPath" ||
			e.name.Local == "mask" || e.name.Local == "symbol" || e.name.Local == "pattern" ||
			e.name.Local == "SVGTestCase" || e.name.Local == "OperatorScript" ||
			e.name.Local == "Paragraph" || e.name.Local == "title" || e.name.Local == "desc" ||
			e.name.Local == "metadata" {
			return
		}
		if tr := attr(e, "transform"); tr != "" {
			m = parseTransform(tr).then(m)
		}
		if e.name.Local == "image" {
			if drawImage(dst, e, m, scale) {
				drawn = true
			}
			return
		}
		for _, k := range e.kids {
			walk(k, m, hidden)
		}
	}
	walk(root, rootM, false)
	return drawn
}

func drawImage(dst *image.RGBA, e *elem, m affine, scale float64) bool {
	href := attr(e, "href")
	data, ok := dataURI(href)
	if !ok {
		if strings.Contains(href, "://") {
			return false
		}
		data, ok = func() ([]byte, bool) {
			b, err := os.ReadFile(href)
			return b, err == nil
		}()
	}
	if !ok {
		return false
	}
	d, err := img.Decode(data)
	if err != nil || d.W <= 0 || d.H <= 0 {
		return false
	}
	src := &image.RGBA{Pix: d.Pix, Stride: d.W * 4, Rect: image.Rect(0, 0, d.W, d.H)}
	x, _ := lengthPx(attr(e, "x"))
	y, _ := lengthPx(attr(e, "y"))
	w, wok := lengthPx(attr(e, "width"))
	h, hok := lengthPx(attr(e, "height"))
	if !wok {
		w = float64(d.W) / scale
	}
	if !hok {
		h = float64(d.H) / scale
	}
	if w <= 0 || h <= 0 {
		return false
	}
	sx, sy := w/float64(d.W), h/float64(d.H)
	if !strings.Contains(attr(e, "preserveAspectRatio"), "none") {
		s := sx
		if sy < s {
			s = sy
		}
		sx, sy = s, s
		x += (w - float64(d.W)*sx) / 2
		y += (h - float64(d.H)*sy) / 2
	}
	local := affine{sx, 0, 0, sy, x, y}.then(m)
	compositeTransformed(dst, src, local, 0)
	return true
}

func dataURI(href string) ([]byte, bool) {
	if !strings.HasPrefix(href, "data:") {
		return nil, false
	}
	comma := strings.IndexByte(href, ',')
	if comma < 0 || !strings.Contains(href[:comma], ";base64") {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(href[comma+1:])
	if err != nil {
		return nil, false
	}
	return b, true
}
