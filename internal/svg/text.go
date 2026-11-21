//go:build all || svg

package svg

import (
	"errors"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"

	srw "github.com/srwiley/oksvg"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type affine [6]float64

func (a affine) apply(x, y float64) (float64, float64) {
	return a[0]*x + a[2]*y + a[4], a[1]*x + a[3]*y + a[5]
}

// then applies a first and b second.
func (a affine) then(b affine) affine {
	return affine{
		b[0]*a[0] + b[2]*a[1],
		b[1]*a[0] + b[3]*a[1],
		b[0]*a[2] + b[2]*a[3],
		b[1]*a[2] + b[3]*a[3],
		b[0]*a[4] + b[2]*a[5] + b[4],
		b[1]*a[4] + b[3]*a[5] + b[5],
	}
}

func invert(a affine) (affine, bool) {
	det := a[0]*a[3] - a[1]*a[2]
	if math.Abs(det) < 1e-12 {
		return affine{}, false
	}
	return affine{
		a[3] / det,
		-a[1] / det,
		-a[2] / det,
		a[0] / det,
		(a[2]*a[5] - a[3]*a[4]) / det,
		(a[1]*a[4] - a[0]*a[5]) / det,
	}, true
}

func parseTransform(s string) affine {
	var out affine = affine{1, 0, 0, 1, 0, 0}
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == ',' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		open := strings.IndexByte(s[i:], '(')
		if open < 0 {
			break
		}
		open += i
		close := strings.IndexByte(s[open:], ')')
		if close < 0 {
			break
		}
		close += open
		name := strings.ToLower(strings.TrimSpace(s[i:open]))
		var args []float64
		for _, p := range strings.FieldsFunc(s[open+1:close], func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		}) {
			if v, err := strconv.ParseFloat(p, 64); err == nil {
				args = append(args, v)
			}
		}
		t := affine{1, 0, 0, 1, 0, 0}
		switch name {
		case "translate":
			if len(args) > 0 {
				t[4] = args[0]
			}
			if len(args) > 1 {
				t[5] = args[1]
			}
		case "scale":
			if len(args) > 0 {
				t[0] = args[0]
				t[3] = args[0]
			}
			if len(args) > 1 {
				t[3] = args[1]
			}
		case "rotate":
			if len(args) > 0 {
				r := args[0] * math.Pi / 180
				c, s := math.Cos(r), math.Sin(r)
				t = affine{c, s, -s, c, 0, 0}
				if len(args) > 2 {
					cx, cy := args[1], args[2]
					t = affine{1, 0, 0, 1, cx, cy}.then(t).then(affine{1, 0, 0, 1, -cx, -cy})
				}
			}
		case "skewx":
			if len(args) > 0 {
				t[2] = math.Tan(args[0] * math.Pi / 180)
			}
		case "skewy":
			if len(args) > 0 {
				t[1] = math.Tan(args[0] * math.Pi / 180)
			}
		case "matrix":
			if len(args) >= 6 {
				copy(t[:], args[:6])
			}
		}
		out = t.then(out)
		i = close + 1
	}
	return out
}

var (
	goFaceOnce       sync.Once
	goFaceRegular    *opentype.Font
	goFaceBold       *opentype.Font
	goFaceItalic     *opentype.Font
	goFaceBoldItalic *opentype.Font
)

func svgFace(size float64, bold, italic bool) (font.Face, error) {
	goFaceOnce.Do(func() {
		goFaceRegular, _ = opentype.Parse(goregular.TTF)
		goFaceBold, _ = opentype.Parse(gobold.TTF)
		goFaceItalic, _ = opentype.Parse(goitalic.TTF)
		goFaceBoldItalic, _ = opentype.Parse(gobolditalic.TTF)
	})
	f := goFaceRegular
	switch {
	case bold && italic:
		f = goFaceBoldItalic
	case bold:
		f = goFaceBold
	case italic:
		f = goFaceItalic
	}
	if f == nil || size <= 0 {
		return nil, errors.New("svg: no text face")
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
}

type textStyle struct {
	fill          string
	fontSize      float64
	anchor        string
	opacity       float64
	fillOpacity   float64
	letterSpacing float64
	stroke        string
	strokeWidth   float64
	bold          bool
	italic        bool
	hidden        bool
}

func styleAt(e *elem, sheet *css, base textStyle) textStyle {
	s := base
	if s.fontSize <= 0 {
		s.fontSize = 16
	}
	if s.opacity <= 0 {
		s.opacity = 1
	}
	if s.fillOpacity <= 0 {
		s.fillOpacity = 1
	}
	if s.fill == "" {
		s.fill = "#000000"
	}
	set := func(m map[string]string) {
		if v, ok := m["fill"]; ok {
			s.fill = v
		}
		if v, ok := m["font-size"]; ok {
			if n, ok := lengthPx(v); ok {
				s.fontSize = n
			}
		}
		if v, ok := m["text-anchor"]; ok {
			s.anchor = strings.TrimSpace(v)
		}
		if v, ok := m["opacity"]; ok {
			if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				s.opacity = n
			}
		}
		if v, ok := m["fill-opacity"]; ok {
			if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				s.fillOpacity = n
			}
		}
		if v, ok := m["letter-spacing"]; ok {
			if n, ok := lengthPx(v); ok {
				s.letterSpacing = n
			}
		}
		if v, ok := m["stroke"]; ok {
			s.stroke = v
		}
		if v, ok := m["stroke-width"]; ok {
			if n, ok := lengthPx(v); ok {
				s.strokeWidth = n
			}
		}
		if v, ok := m["font-weight"]; ok {
			w := strings.TrimSpace(v)
			s.bold = w == "bold" || w == "bolder" || (func() bool {
				n, err := strconv.Atoi(w)
				return err == nil && n >= 600
			})()
		}
		if v, ok := m["font-style"]; ok {
			s.italic = strings.TrimSpace(v) == "italic"
		}
		if v, ok := m["display"]; ok && strings.TrimSpace(v) == "none" {
			s.hidden = true
		}
		if v, ok := m["visibility"]; ok && strings.TrimSpace(v) == "hidden" {
			s.hidden = true
		}
	}
	attrs := map[string]string{}
	for _, a := range e.attrs {
		attrs[a.Name.Local] = a.Value
	}
	set(attrs)
	set(sheet.style(e))
	set(inlineStyle(e))
	return s
}

// drawTexts paints <text> elements over the shape pass. It uses one embedded
// font and supports x/y, font-size, text-anchor, fill, opacity, transforms and
// tspan text; it is deliberately a text fallback, not a full text layout engine.
func drawTexts(dst *image.RGBA, root *elem, sheet *css, scale float64, ink uint32) {
	if root == nil {
		return
	}
	rootM := affine{scale, 0, 0, scale, 0, 0}
	var walk func(e *elem, m affine, st textStyle, hidden bool)
	walk = func(e *elem, m affine, st textStyle, hidden bool) {
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
		st = styleAt(e, sheet, st)
		if st.hidden {
			return
		}
		if e.name.Local == "text" {
			drawText(dst, e, m, st, scale, ink)
			return
		}
		for _, k := range e.kids {
			walk(k, m, st, hidden)
		}
	}
	walk(root, rootM, textStyle{}, false)
}

func drawText(dst *image.RGBA, e *elem, m affine, st textStyle, scale float64, ink uint32) {
	text := strings.TrimSpace(collectText(e))
	if text == "" || st.fill == "none" {
		return
	}
	size := st.fontSize * scale
	if size <= 0 || size > 4096 {
		return
	}
	face, err := svgFace(size, st.bold, st.italic)
	if err != nil {
		return
	}
	defer face.Close()
	b, adv := font.BoundString(face, text)
	bw := b.Max.X.Ceil() - b.Min.X.Floor()
	bh := b.Max.Y.Ceil() - b.Min.Y.Floor()
	if bw <= 0 || bh <= 0 {
		return
	}
	runes := []rune(text)
	spacingPx := st.letterSpacing * scale
	total := float64(adv) / 64
	if len(runes) > 1 && spacingPx != 0 {
		total += spacingPx * float64(len(runes)-1)
	}
	strokePx := st.strokeWidth * scale
	strokePad := int(math.Ceil(strokePx / 2))
	pad := 2 + strokePad
	tw, th := int(total)+2*pad, bh+2*pad
	if tw <= 0 || th <= 0 {
		return
	}
	x, _ := lengthPx(attr(e, "x"))
	if dx, ok := lengthPx(attr(e, "dx")); ok {
		x += dx
	}
	y, _ := lengthPx(attr(e, "y"))
	if dy, ok := lengthPx(attr(e, "dy")); ok {
		y += dy
	}
	switch st.anchor {
	case "middle":
		x -= total / scale / 2
	case "end":
		x -= total / scale
	}
	tmp := image.NewRGBA(image.Rect(0, 0, tw, th))
	col := textColor(st, ink)
	if col.A == 0 {
		return
	}
	dotX, dotY := pad-b.Min.X.Ceil(), pad-b.Min.Y.Ceil()
	d := font.Drawer{
		Dst:  tmp,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(dotX, dotY),
	}
	drawRun := func(offX, offY int, c color.NRGBA) {
		d.Src = image.NewUniform(c)
		if spacingPx == 0 || len(runes) < 2 {
			d.Dot = fixed.P(dotX+offX, dotY+offY)
			d.DrawString(text)
			return
		}
		x := dotX + offX
		for _, r := range runes {
			d.Dot = fixed.P(x, dotY+offY)
			d.DrawString(string(r))
			x += int(font.MeasureString(face, string(r)))/64 + int(spacingPx+0.5)
		}
	}
	if st.stroke != "" && st.strokeWidth > 0 {
		stroke := svgColor(st.stroke, ink, st.opacity)
		if stroke.A > 0 {
			rad := int(math.Ceil(strokePx / 2))
			for i := -rad; i <= rad; i += maxInt(1, rad) {
				for j := -rad; j <= rad; j += maxInt(1, rad) {
					if i == 0 && j == 0 {
						continue
					}
					drawRun(i, j, stroke)
				}
			}
		}
	}
	drawRun(0, 0, col)
	// The face is a pixel size already multiplied by scale, so map the temp
	// bitmap back into SVG units before the local-to-canvas matrix places it.
	invScale := 1 / scale
	tempM := affine{invScale, 0, 0, invScale, x - float64(pad)*invScale, y - float64(pad)*invScale}.then(m)
	compositeTransformed(dst, tmp, tempM, 0)
}

func collectText(e *elem) string {
	var b strings.Builder
	var walk func(*elem)
	walk = func(n *elem) {
		if n.name.Local == "text" || n.name.Local == "tspan" {
			b.WriteString(n.text)
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(e)
	return b.String()
}

func textColor(st textStyle, ink uint32) color.NRGBA {
	c := svgColor(st.fill, ink, st.opacity*st.fillOpacity)
	if c.A == 0 {
		c = color.NRGBA{0, 0, 0, 255}
	}
	return c
}

func svgColor(v string, ink uint32, opacity float64) color.NRGBA {
	v = strings.TrimSpace(v)
	if v == "currentColor" {
		v = hexRGB(ink)
	}
	c, err := srw.ParseSVGColor(v)
	if err != nil || c == nil {
		return color.NRGBA{}
	}
	r, g, b, a := c.RGBA()
	alpha := float64(a) / 65535 * opacity
	return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(alpha*255 + 0.5)}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// compositeTransformed maps a small bitmap into dst through a local-to-canvas
// affine. It is a bilinear sampler, which is enough for text and images and
// keeps rotation and skew support without another renderer.
func compositeTransformed(dst *image.RGBA, src *image.RGBA, m affine, pad int) {
	inv, ok := invert(m)
	if !ok {
		return
	}
	corners := [][2]float64{{0, 0}, {float64(src.Rect.Dx()), 0}, {0, float64(src.Rect.Dy())}, {float64(src.Rect.Dx()), float64(src.Rect.Dy())}}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range corners {
		px, py := m.apply(c[0], c[1])
		minX, minY = math.Min(minX, px), math.Min(minY, py)
		maxX, maxY = math.Max(maxX, px), math.Max(maxY, py)
	}
	x0, y0 := int(math.Floor(minX)), int(math.Floor(minY))
	x1, y1 := int(math.Ceil(maxX)), int(math.Ceil(maxY))
	if x0 < dst.Rect.Min.X {
		x0 = dst.Rect.Min.X
	}
	if y0 < dst.Rect.Min.Y {
		y0 = dst.Rect.Min.Y
	}
	if x1 > dst.Rect.Max.X {
		x1 = dst.Rect.Max.X
	}
	if y1 > dst.Rect.Max.Y {
		y1 = dst.Rect.Max.Y
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			lx, ly := inv.apply(float64(x)+0.5, float64(y)+0.5)
			sx, sy := lx+float64(pad), ly+float64(pad)
			if sx < 0 || sy < 0 || sx >= float64(src.Rect.Dx()) || sy >= float64(src.Rect.Dy()) {
				continue
			}
			ix, iy := int(sx), int(sy)
			fx, fy := sx-float64(ix), sy-float64(iy)
			var acc [4]float64
			for j := 0; j < 2; j++ {
				for i := 0; i < 2; i++ {
					px, py := ix+i, iy+j
					if px >= src.Rect.Dx() || py >= src.Rect.Dy() {
						continue
					}
					wx, wy := 1-fx, 1-fy
					if i == 1 {
						wx = fx
					}
					if j == 1 {
						wy = fy
					}
					ws := wx * wy
					o := py*src.Stride + px*4
					acc[0] += float64(src.Pix[o]) * ws
					acc[1] += float64(src.Pix[o+1]) * ws
					acc[2] += float64(src.Pix[o+2]) * ws
					acc[3] += float64(src.Pix[o+3]) * ws
				}
			}
			if acc[3] <= 0 {
				continue
			}
			drawPixel(dst, x, y, color.NRGBA{uint8(acc[0]), uint8(acc[1]), uint8(acc[2]), uint8(acc[3])})
		}
	}
}

func drawPixel(dst *image.RGBA, x, y int, c color.NRGBA) {
	if !(image.Point{x, y}).In(dst.Rect) {
		return
	}
	o := dst.PixOffset(x, y)
	a := uint32(c.A)
	if a == 0 {
		return
	}
	inv := 255 - a
	dst.Pix[o] = uint8((uint32(c.R)*a + uint32(dst.Pix[o])*inv) / 255)
	dst.Pix[o+1] = uint8((uint32(c.G)*a + uint32(dst.Pix[o+1])*inv) / 255)
	dst.Pix[o+2] = uint8((uint32(c.B)*a + uint32(dst.Pix[o+2])*inv) / 255)
	dst.Pix[o+3] = uint8((uint32(c.A)*a + uint32(dst.Pix[o+3])*inv) / 255)
}
