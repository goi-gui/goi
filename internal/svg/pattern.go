//go:build all || svg

package svg

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/draw"
	"strings"
)

type patternLayer struct {
	tile []byte
	mask []byte
	x, y float64
	w, h float64
	bbox bool
	vbW  float64
	vbH  float64
}

// collectPatterns turns the top-level shapes that fill with a pattern into
// layers the caller can tile over the background. It also returns a document
// with those shapes removed, so oksvg does not paint their fallback black.
func collectPatterns(root *elem, sheet *css) ([]patternLayer, []byte) {
	if root == nil {
		return nil, nil
	}
	var layers []patternLayer
	var clone func(e *elem) *elem
	clone = func(e *elem) *elem {
		if e != root && isDrawable(e) {
			fill := fillValue(e, sheet)
			if strings.HasPrefix(strings.TrimSpace(fill), "url(") {
				def := findElement(root, clipURLID(fill))
				if def != nil && def.name.Local == "pattern" {
					units := strings.TrimSpace(attr(def, "patternUnits"))
					bbox := units == "" || units == "objectBoundingBox"
					ok := units == "userSpaceOnUse" || (bbox && strings.TrimSpace(attr(def, "patternContentUnits")) == "objectBoundingBox")
					if ok {
						w, wok := lengthPx(attr(def, "width"))
						h, hok := lengthPx(attr(def, "height"))
						if wok && hok && w > 0 && h > 0 {
							tile := patternDocument(root, sheet, def, w, h)
							mask := patternMask(root, sheet, e)
							if tile != nil && mask != nil {
								x, _ := lengthPx(attr(def, "x"))
								y, _ := lengthPx(attr(def, "y"))
								vbW, vbH := w, h
								if bbox {
									vbW, vbH = 1, 1
								}
								layers = append(layers, patternLayer{tile: tile, mask: mask, x: x, y: y, w: w, h: h, bbox: bbox, vbW: vbW, vbH: vbH})
								return nil
							}
						}
					}
				}
			}
		}
		out := cloneElem(e)
		out.kids = out.kids[:0]
		for _, k := range e.kids {
			if c := clone(k); c != nil {
				out.kids = append(out.kids, c)
			}
		}
		return out
	}
	main := clone(root)
	if len(layers) == 0 {
		return nil, nil
	}
	return layers, encodeDoc(main, sheet)
}

func fillValue(e *elem, sheet *css) string {
	v := attr(e, "fill")
	if m := sheet.style(e); m != nil {
		if x, ok := m["fill"]; ok {
			v = x
		}
	}
	if m := inlineStyle(e); m != nil {
		if x, ok := m["fill"]; ok {
			v = x
		}
	}
	return urlOnly(v)
}

func patternDocument(root *elem, sheet *css, def *elem, w, h float64) []byte {
	doc := &elem{name: xml.Name{Local: "svg"}}
	vbW, vbH := w, h
	if strings.TrimSpace(attr(def, "patternContentUnits")) == "objectBoundingBox" {
		vbW, vbH = 1, 1
	}
	doc.attrs = append(doc.attrs, xmlAttr("viewBox", "0 0 "+ftoa(vbW)+" "+ftoa(vbH)))
	for _, a := range root.attrs {
		if a.Name.Local == "viewBox" || a.Name.Local == "width" || a.Name.Local == "height" || a.Name.Local == "clip-path" || a.Name.Local == "mask" {
			continue
		}
		doc.attrs = append(doc.attrs, a)
	}
	for _, k := range root.kids {
		if k.name.Local == "defs" || k.name.Local == "style" {
			doc.kids = append(doc.kids, k)
		}
	}
	for _, k := range def.kids {
		doc.kids = append(doc.kids, cloneElem(k))
	}
	return encodeDoc(doc, sheet)
}

func patternMask(root *elem, sheet *css, target *elem) []byte {
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
		case "style", "class", "fill", "clip-path", "mask", "filter":
			continue
		}
		shape.attrs = append(shape.attrs, a)
	}
	shape.attrs = append(shape.attrs, xmlAttr("fill", "#ffffff"))
	doc.kids = append(doc.kids, shape)
	return encodeDoc(doc, sheet)
}

func encodeDoc(e *elem, sheet *css) []byte {
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	if err := emitElem(enc, e, sheet, 0, true); err != nil {
		return nil
	}
	if err := enc.Flush(); err != nil {
		return nil
	}
	return out.Bytes()
}

func patternOverlay(layers []patternLayer, color uint32, w, h int, scale float64) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for _, lay := range layers {
		mask, err := drawSVG(lay.mask, 0xFFFFFFFF, w, h, 1, 1)
		if err != nil || mask.W != w || mask.H != h {
			continue
		}
		var tw, th, ox, oy int
		if lay.bbox {
			box := alphaBounds(mask.Pix, w, h)
			if box.Dx() <= 0 || box.Dy() <= 0 {
				continue
			}
			tw = int(lay.w*float64(box.Dx()) + 0.5)
			th = int(lay.h*float64(box.Dy()) + 0.5)
			ox = box.Min.X + int(lay.x*float64(box.Dx()))
			oy = box.Min.Y + int(lay.y*float64(box.Dy()))
		} else {
			tw = int(float64(lay.w)*scale + 0.5)
			th = int(float64(lay.h)*scale + 0.5)
			ox = int(float64(lay.x) * scale)
			oy = int(float64(lay.y) * scale)
		}
		if tw < 1 || th < 1 {
			continue
		}
		tile, err := drawSVG(lay.tile, color, tw, th, lay.vbW, lay.vbH)
		if err != nil || tile.W <= 0 || tile.H <= 0 {
			continue
		}
		tiled := image.NewRGBA(image.Rect(0, 0, w, h))
		tileImg := &image.RGBA{Pix: tile.Pix, Stride: tile.W * 4, Rect: image.Rect(0, 0, tile.W, tile.H)}
		startX, startY := ox%tw, oy%th
		if startX > 0 {
			startX -= tw
		}
		if startY > 0 {
			startY -= th
		}
		for y := startY; y < h; y += th {
			for x := startX; x < w; x += tw {
				draw.Draw(tiled, image.Rect(x, y, x+tw, y+th), tileImg, image.Point{}, draw.Over)
			}
		}
		for i := 0; i < len(tiled.Pix); i += 4 {
			tiled.Pix[i+3] = byte(uint16(tiled.Pix[i+3]) * uint16(mask.Pix[i+3]) / 255)
		}
		draw.Draw(out, out.Bounds(), tiled, image.Point{}, draw.Over)
	}
	return out
}
