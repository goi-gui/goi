//go:build all || svg

package svg

import (
	"encoding/xml"
	"image"
	"image/draw"
	"math"
	"strconv"
	"strings"

	"github.com/srwiley/oksvg"
)

// blurRadius reads a Gaussian blur filter on the root or a single root group.
// It is the common "soft shadow" filter; other filter primitives are left to
// the editor, because applying them per element needs a layer per shape.
func blurRadius(root *elem) (sx, sy float64, ok bool) {
	if root == nil || root.name.Local != "svg" {
		return 0, 0, false
	}
	target := root
	ref := attr(target, "filter")
	if ref == "" {
		var drawable []*elem
		for _, k := range root.kids {
			if isDrawable(k) {
				drawable = append(drawable, k)
			}
		}
		if len(drawable) != 1 {
			return 0, 0, false
		}
		target = drawable[0]
		ref = attr(target, "filter")
	}
	if ref == "" {
		return 0, 0, false
	}
	def := findElement(root, clipURLID(ref))
	if def == nil || def.name.Local != "filter" {
		return 0, 0, false
	}
	for _, k := range def.kids {
		if k.name.Local != "feGaussianBlur" {
			continue
		}
		parts := strings.FieldsFunc(attr(k, "stdDeviation"), func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		if len(parts) == 0 {
			return 0, 0, false
		}
		sx, err := strconv.ParseFloat(parts[0], 64)
		if err != nil || sx <= 0 {
			return 0, 0, false
		}
		sy = sx
		if len(parts) > 1 {
			if v, err := strconv.ParseFloat(parts[1], 64); err == nil && v > 0 {
				sy = v
			}
		}
		return sx, sy, true
	}
	return 0, 0, false
}

// blurRGBA is three box passes, which approximate a Gaussian closely enough
// for a shadow and stay O(pixels) at any radius.
func blurRGBA(pix []byte, w, h int, sigmaX, sigmaY float64) {
	if sigmaX <= 0 && sigmaY <= 0 {
		return
	}
	rx := int(math.Ceil(sigmaX * 1.5))
	ry := int(math.Ceil(sigmaY * 1.5))
	if rx < 1 && ry < 1 {
		return
	}
	tmp := make([]byte, len(pix))
	for pass := 0; pass < 3; pass++ {
		if rx > 0 {
			boxX(pix, tmp, w, h, rx)
			copy(pix, tmp)
		}
		if ry > 0 {
			boxY(pix, tmp, w, h, ry)
			copy(pix, tmp)
		}
	}
}

func boxX(src, dst []byte, w, h, r int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum [4]int
			n := 0
			for k := -r; k <= r; k++ {
				xx := x + k
				if xx < 0 || xx >= w {
					continue
				}
				o := (y*w + xx) * 4
				sum[0] += int(src[o])
				sum[1] += int(src[o+1])
				sum[2] += int(src[o+2])
				sum[3] += int(src[o+3])
				n++
			}
			o := (y*w + x) * 4
			dst[o], dst[o+1], dst[o+2], dst[o+3] = byte(sum[0]/n), byte(sum[1]/n), byte(sum[2]/n), byte(sum[3]/n)
		}
	}
}

func boxY(src, dst []byte, w, h, r int) {
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			var sum [4]int
			n := 0
			for k := -r; k <= r; k++ {
				yy := y + k
				if yy < 0 || yy >= h {
					continue
				}
				o := (yy*w + x) * 4
				sum[0] += int(src[o])
				sum[1] += int(src[o+1])
				sum[2] += int(src[o+2])
				sum[3] += int(src[o+3])
				n++
			}
			o := (y*w + x) * 4
			dst[o], dst[o+1], dst[o+2], dst[o+3] = byte(sum[0]/n), byte(sum[1]/n), byte(sum[2]/n), byte(sum[3]/n)
		}
	}
}

type dropShadow struct {
	dx, dy  float64
	sigma   float64
	opacity float64
	color   uint32
}

func dropShadowOf(root *elem) *dropShadow {
	if root == nil || root.name.Local != "svg" {
		return nil
	}
	target := root
	ref := attr(target, "filter")
	if ref == "" {
		var drawable []*elem
		for _, k := range root.kids {
			if isDrawable(k) {
				drawable = append(drawable, k)
			}
		}
		if len(drawable) != 1 {
			return nil
		}
		target = drawable[0]
		ref = attr(target, "filter")
	}
	if ref == "" {
		return nil
	}
	def := findElement(root, clipURLID(ref))
	if def == nil || def.name.Local != "filter" {
		return nil
	}
	for _, k := range def.kids {
		if k.name.Local != "feDropShadow" {
			continue
		}
		out := &dropShadow{opacity: 1, color: 0x000000FF}
		out.dx, _ = lengthPx(attr(k, "dx"))
		out.dy, _ = lengthPx(attr(k, "dy"))
		parts := strings.Fields(attr(k, "stdDeviation"))
		if len(parts) > 0 {
			if v, err := strconv.ParseFloat(parts[0], 64); err == nil && v > 0 {
				out.sigma = v
			}
		}
		if v := strings.TrimSpace(attr(k, "flood-opacity")); v != "" {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				out.opacity = n
			}
		}
		if v := strings.TrimSpace(attr(k, "flood-color")); v != "" {
			if c, err := oksvg.ParseSVGColor(v); err == nil && c != nil {
				r, g, b, _ := c.RGBA()
				out.color = uint32(r>>8)<<24 | uint32(g>>8)<<16 | uint32(b>>8)<<8 | 0xFF
			}
		}
		return out
	}
	return nil
}

func dropShadowImage(pix []byte, w, h int, sh *dropShadow, scale float64) []byte {
	if sh == nil || sh.opacity <= 0 {
		return nil
	}
	dx := int(math.Round(sh.dx * scale))
	dy := int(math.Round(sh.dy * scale))
	out := make([]byte, len(pix))
	r := byte(sh.color >> 24)
	g := byte(sh.color >> 16)
	b := byte(sh.color >> 8)
	for y := 0; y < h; y++ {
		sy := y - dy
		if sy < 0 || sy >= h {
			continue
		}
		for x := 0; x < w; x++ {
			sx := x - dx
			if sx < 0 || sx >= w {
				continue
			}
			so := (sy*w + sx) * 4
			a := float64(pix[so+3]) / 255 * sh.opacity
			o := (y*w + x) * 4
			out[o], out[o+1], out[o+2], out[o+3] = r, g, b, byte(a*255+0.5)
		}
	}
	blurRGBA(out, w, h, sh.sigma*scale, sh.sigma*scale)
	return out
}

type filterLayer struct {
	doc    []byte
	blurX  float64
	blurY  float64
	shadow *dropShadow
	region [4]float64
}

// collectFilters handles a filter on an ordinary element by rendering that
// element as a layer and filtering the layer. Root-level filters are handled
// directly; this path covers a row of shapes that each carry their own shadow or
// blur, which is what a design tool exports for a blurred illustration.
func collectFilters(root *elem, sheet *css) ([]filterLayer, []byte) {
	if root == nil {
		return nil, nil
	}
	var layers []filterLayer
	var clone func(e *elem, m affine) *elem
	clone = func(e *elem, m affine) *elem {
		if tr := attr(e, "transform"); tr != "" {
			m = parseTransform(tr).then(m)
		}
		if e != root {
			if spec, ok := filterSpecFor(root, e); ok {
				doc := filterDocument(root, sheet, e, m)
				if doc != nil {
					layers = append(layers, filterLayer{doc: doc, blurX: spec.blurX, blurY: spec.blurY, shadow: spec.shadow, region: spec.region})
					return nil
				}
			}
		}
		out := cloneElem(e)
		out.kids = out.kids[:0]
		for _, k := range e.kids {
			if c := clone(k, m); c != nil {
				out.kids = append(out.kids, c)
			}
		}
		return out
	}
	main := clone(root, affine{1, 0, 0, 1, 0, 0})
	if len(layers) == 0 {
		return nil, nil
	}
	return layers, encodeDoc(main, sheet)
}

type filterSpec struct {
	blurX, blurY float64
	shadow       *dropShadow
	region       [4]float64
}

func filterSpecFor(root, e *elem) (filterSpec, bool) {
	ref := filterValue(e)
	if ref == "" {
		return filterSpec{}, false
	}
	def := findElement(root, clipURLID(ref))
	if def == nil || def.name.Local != "filter" {
		return filterSpec{}, false
	}
	out := filterSpec{region: [4]float64{-0.1, -0.1, 1.2, 1.2}}
	parseFilterRegion(def, &out.region)
	for _, k := range def.kids {
		switch k.name.Local {
		case "feGaussianBlur":
			parts := strings.Fields(attr(k, "stdDeviation"))
			if len(parts) > 0 {
				if v, err := strconv.ParseFloat(parts[0], 64); err == nil && v > 0 {
					out.blurX = v
					out.blurY = v
					if len(parts) > 1 {
						if h, err := strconv.ParseFloat(parts[1], 64); err == nil && h > 0 {
							out.blurY = h
						}
					}
				}
			}
		case "feDropShadow":
			sh := &dropShadow{opacity: 1, color: 0x000000FF}
			sh.dx, _ = lengthPx(attr(k, "dx"))
			sh.dy, _ = lengthPx(attr(k, "dy"))
			parts := strings.Fields(attr(k, "stdDeviation"))
			if len(parts) > 0 {
				if v, err := strconv.ParseFloat(parts[0], 64); err == nil && v > 0 {
					sh.sigma = v
				}
			}
			if v := strings.TrimSpace(attr(k, "flood-opacity")); v != "" {
				if n, err := strconv.ParseFloat(v, 64); err == nil {
					sh.opacity = n
				}
			}
			if v := strings.TrimSpace(attr(k, "flood-color")); v != "" {
				if c, err := oksvg.ParseSVGColor(v); err == nil && c != nil {
					r, g, b, _ := c.RGBA()
					sh.color = uint32(r>>8)<<24 | uint32(g>>8)<<16 | uint32(b>>8)<<8 | 0xFF
				}
			}
			out.shadow = sh
		}
	}
	return out, out.blurX > 0 || out.blurY > 0 || out.shadow != nil
}

func filterValue(e *elem) string {
	v := attr(e, "filter")
	if m := inlineStyle(e); m != nil {
		if x, ok := m["filter"]; ok {
			v = x
		}
	}
	return urlOnly(v)
}

func filterDocument(root *elem, sheet *css, target *elem, m affine) []byte {
	doc := &elem{name: xml.Name{Local: "svg"}}
	for _, a := range root.attrs {
		if a.Name.Local == "clip-path" || a.Name.Local == "mask" {
			continue
		}
		doc.attrs = append(doc.attrs, a)
	}
	for _, k := range root.kids {
		if k.name.Local == "defs" || k.name.Local == "style" {
			doc.kids = append(doc.kids, k)
		}
	}
	shape := cloneElem(target)
	shape.attrs = shape.attrs[:0]
	for _, a := range target.attrs {
		switch a.Name.Local {
		case "style":
			if v := withoutStyleProp(a.Value, "filter"); v != "" {
				shape.attrs = append(shape.attrs, xmlAttr("style", v))
			}
			continue
		case "filter", "clip-path", "mask":
			continue
		}
		shape.attrs = append(shape.attrs, a)
	}
	if m != (affine{1, 0, 0, 1, 0, 0}) {
		shape.attrs = append(shape.attrs, xmlAttr("transform", matrixString(m)))
	}
	doc.kids = append(doc.kids, shape)
	return encodeDoc(doc, sheet)
}

func matrixString(m affine) string {
	return "matrix(" + ftoa(m[0]) + "," + ftoa(m[1]) + "," + ftoa(m[2]) + "," + ftoa(m[3]) + "," + ftoa(m[4]) + "," + ftoa(m[5]) + ")"
}

func drawFilterLayers(dst *image.RGBA, layers []filterLayer, color uint32, w, h int, vbW, vbH float64) {
	for _, lay := range layers {
		img, err := drawSVG(lay.doc, color, w, h, vbW, vbH)
		if err != nil {
			continue
		}
		scale := float64(w) / vbW
		if lay.shadow != nil {
			if sh := dropShadowImage(img.Pix, w, h, lay.shadow, scale); sh != nil {
				base := image.NewRGBA(image.Rect(0, 0, w, h))
				copy(base.Pix, sh)
				draw.Draw(base, base.Bounds(),
					&image.RGBA{Pix: img.Pix, Stride: img.W * 4, Rect: image.Rect(0, 0, img.W, img.H)},
					image.Point{}, draw.Over)
				img.Pix = base.Pix
			}
		}
		bounds := alphaBounds(img.Pix, w, h)
		if lay.blurX > 0 || lay.blurY > 0 {
			blurRGBA(img.Pix, w, h, lay.blurX*scale, lay.blurY*scale)
		}
		clipFilterRegion(img.Pix, w, h, bounds, lay.region)
		draw.Draw(dst, dst.Bounds(),
			&image.RGBA{Pix: img.Pix, Stride: img.W * 4, Rect: image.Rect(0, 0, img.W, img.H)},
			image.Point{}, draw.Over)
	}
}

func withoutStyleProp(style, drop string) string {
	decls := parseDecls(style)
	delete(decls, drop)
	var b strings.Builder
	for k, v := range decls {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(v)
		b.WriteByte(';')
	}
	return b.String()
}

func parseFilterRegion(e *elem, region *[4]float64) {
	keys := [4]string{"x", "y", "width", "height"}
	for i, k := range keys {
		v := strings.TrimSpace(attr(e, k))
		if v == "" {
			continue
		}
		if strings.HasSuffix(v, "%") {
			if n, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64); err == nil {
				region[i] = n / 100
			}
		} else if n, err := strconv.ParseFloat(v, 64); err == nil {
			region[i] = n
		}
	}
}

func alphaBounds(pix []byte, w, h int) image.Rectangle {
	b := image.Rectangle{Min: image.Point{X: w, Y: h}}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if pix[(y*w+x)*4+3] == 0 {
				continue
			}
			if x < b.Min.X {
				b.Min.X = x
			}
			if y < b.Min.Y {
				b.Min.Y = y
			}
			if x >= b.Max.X {
				b.Max.X = x + 1
			}
			if y >= b.Max.Y {
				b.Max.Y = y + 1
			}
		}
	}
	if b.Max.X <= b.Min.X || b.Max.Y <= b.Min.Y {
		return image.Rect(0, 0, 0, 0)
	}
	return b
}

func clipFilterRegion(pix []byte, w, h int, box image.Rectangle, region [4]float64) {
	bw, bh := float64(box.Dx()), float64(box.Dy())
	if bw <= 0 || bh <= 0 {
		return
	}
	x0 := int(float64(box.Min.X) + region[0]*bw)
	y0 := int(float64(box.Min.Y) + region[1]*bh)
	x1 := int(float64(box.Min.X) + (region[0]+region[2])*bw)
	y1 := int(float64(box.Min.Y) + (region[1]+region[3])*bh)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= x0 && x < x1 && y >= y0 && y < y1 {
				continue
			}
			pix[(y*w+x)*4+3] = 0
		}
	}
}
