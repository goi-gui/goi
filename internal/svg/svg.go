//go:build all || svg

// Package svg parses SVG files and rasterizes them into the renderer's pixel
// format with currentColor replaced by the colour the design chose.
package svg

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"regexp"
	"strconv"
	"time"

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
// again without reading the file, and the element tree so an animation can be
// re-rendered at a moment in time.
type SVG struct {
	src   []byte
	w     float64
	h     float64
	tree  *elem
	css   *css
	anims []*anim
	// dur is the longest animation in the file, zero for a still document.
	dur time.Duration
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
	s := &SVG{src: b, w: w, h: h}
	// The tree is only needed when the file animates or keeps its paint in a
	// stylesheet, and parsing it costs a second walk of the document.
	if bytes.Contains(b, []byte("<animate")) || bytes.Contains(b, []byte("<style")) {
		if tree, err := parseTree(b); err == nil {
			anims, dur := collectAnims(tree)
			s.tree, s.css, s.anims, s.dur = tree, collectCSS(tree), anims, dur
			// A file with only a stylesheet still needs the rewrite, so that the
			// classes become attributes the rasterizer understands.
			if len(anims) == 0 && s.css.empty() {
				s.tree, s.css = nil, nil
			}
		}
	}
	return s, nil
}

// Duration is the longest animation in the file; zero means it is still.
func (s *SVG) Duration() time.Duration { return s.dur }

// Animated reports whether the file declares a SMIL animation this package can
// play.
func (s *SVG) Animated() bool { return s.dur > 0 }

// PlayLength is how long the document animates before it holds its last frame.
// Zero means at least one animation repeats forever.
func (s *SVG) PlayLength() time.Duration {
	var longest time.Duration
	for _, a := range s.anims {
		if a.repeat == 0 {
			return 0
		}
		if end := a.begin + a.dur; end > longest {
			longest = end
		}
	}
	return longest
}

// Intrinsic is the viewBox size in logical pixels.
func (s *SVG) Intrinsic() (w, h float64) { return s.w, s.h }

// Render rasterizes the file with color as currentColor. The alpha channel of
// color becomes the drawing opacity, so a half-transparent design colour keeps
// working even though SVG hex colours have no alpha.
func (s *SVG) Render(color uint32) (*img.Data, error) {
	return s.RenderAt(color, 0)
}

// RenderAt rasterizes the file as it looks at elapsed, applying its SMIL
// animations first. A file with no animation ignores elapsed.
//
// shortcut: the document is re-parsed for every frame, so an animated SVG costs
// a parse and a raster per repaint. Upgrade with a retained oksvg icon and a
// transform-only fast path if a large animated SVG ever shows up in a profile.
func (s *SVG) RenderAt(color uint32, elapsed time.Duration) (*img.Data, error) {
	return s.RenderAtLoop(color, elapsed, true)
}

// RenderAtLoop is RenderAt with a loop choice: false plays each animation once
// and holds its last frame, whatever repeatCount the file declares.
func (s *SVG) RenderAtLoop(color uint32, elapsed time.Duration, loop bool) (*img.Data, error) {
	src := defaultRectRy(s.renderAt(elapsed, loop))
	pixelW, _ := s.pixels()
	src = scaleStrokeWidths(src, float64(pixelW)/s.w)
	icon, err := oksvg.ReadReplacingCurrentColor(bytes.NewReader(src), hexRGB(color))
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

var (
	rectTagRE    = regexp.MustCompile(`(?is)<rect\b[^>]*>`)
	rectRXRE     = regexp.MustCompile(`(?i)\brx\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	rectRYRE     = regexp.MustCompile(`(?i)\bry\s*=`)
	strokeAttrRE = regexp.MustCompile(`(?i)(\bstroke-width\s*=\s*)(["'])([0-9]*\.?[0-9]+)["']`)
	strokeCSSRE  = regexp.MustCompile(`(?i)(\bstroke-width\s*:\s*)([0-9]*\.?[0-9]+)`)
)

// defaultRectRy fills in the SVG rule that ry defaults to rx. oksvg passes the
// missing ry as zero and draws a square corner, which is why a logo with only
// rx="38" lost its rounded background.
func defaultRectRy(b []byte) []byte {
	return rectTagRE.ReplaceAllFunc(b, func(tag []byte) []byte {
		if rectRYRE.Match(tag) {
			return tag
		}
		m := rectRXRE.FindSubmatch(tag)
		if m == nil {
			return tag
		}
		value := m[1]
		if len(value) == 0 {
			value = m[2]
		}
		if len(value) == 0 {
			return tag
		}
		insert := append([]byte(` ry="`), value...)
		insert = append(insert, '"')
		end := bytes.LastIndexByte(tag, '>')
		if end < 0 {
			return tag
		}
		if end > 0 && tag[end-1] == '/' {
			end--
		}
		out := make([]byte, 0, len(tag)+len(insert))
		out = append(out, tag[:end]...)
		out = append(out, insert...)
		out = append(out, tag[end:]...)
		return out
	})
}

// scaleStrokeWidths compensates for oksvg's target transform: it scales path
// coordinates, but rasterx receives the SVG stroke width in raster pixels. A
// 7px stroke at a 4x raster therefore drew 7px, not 28px, and looked hairline
// after the bitmap was scaled back to the element's size.
func scaleStrokeWidths(b []byte, scale float64) []byte {
	if scale == 1 || scale <= 0 {
		return b
	}
	b = strokeAttrRE.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := strokeAttrRE.FindSubmatch(m)
		v, err := strconv.ParseFloat(string(sub[3]), 64)
		if err != nil {
			return m
		}
		return append(append(append([]byte{}, sub[1]...), sub[2]...),
			append([]byte(strconv.FormatFloat(v*scale, 'f', -1, 64)), sub[2]...)...)
	})
	b = strokeCSSRE.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := strokeCSSRE.FindSubmatch(m)
		v, err := strconv.ParseFloat(string(sub[2]), 64)
		if err != nil {
			return m
		}
		return append(append([]byte{}, sub[1]...), []byte(strconv.FormatFloat(v*scale, 'f', -1, 64))...)
	})
	return b
}

// hexRGB formats the RGB half of 0xRRGGBBAA the way oksvg expects.
func hexRGB(c uint32) string { return fmt.Sprintf("#%06x", c>>8) }
