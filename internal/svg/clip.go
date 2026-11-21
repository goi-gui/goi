//go:build all || svg

package svg

import (
	"bytes"
	"encoding/xml"
	"strings"
)

// clipMaskSource builds a second document that paints a <clipPath>'s geometry
// white. The renderer has no clip support, so the caller multiplies the alpha
// of the finished picture by this mask. It only takes the shape of the tree
// where a root-level group is the one thing being clipped: that is the common
// "logo inside a rounded shape" export, and applying the mask to the whole
// document is exact there; a direct root shape is handled the same way.
func clipMaskSource(root *elem) []byte {
	if root == nil || root.name.Local != "svg" {
		return nil
	}
	target := root
	var drawable []*elem
	for _, k := range root.kids {
		if isDrawable(k) {
			drawable = append(drawable, k)
		}
	}
	clip := attr(target, "clip-path")
	if clip == "" {
		if len(drawable) != 1 {
			return nil
		}
		target = drawable[0]
		clip = attr(target, "clip-path")
	}
	if clip == "" {
		return nil
	}
	id := clipURLID(clip)
	if id == "" {
		return nil
	}
	def := findElement(root, id)
	if def == nil || def.name.Local != "clipPath" {
		return nil
	}
	if strings.TrimSpace(attr(def, "clipPathUnits")) == "objectBoundingBox" {
		return nil
	}
	var shapes []*elem
	for _, k := range def.kids {
		if k.name.Local == "text" || k.name.Local == "image" {
			return nil // the renderer cannot draw these as a mask
		}
		shapes = append(shapes, maskShape(k))
	}
	if len(shapes) == 0 {
		return nil
	}
	maskRoot := &elem{name: xml.Name{Local: "svg"}}
	for _, a := range root.attrs {
		switch a.Name.Local {
		case "clip-path", "mask", "style":
			continue
		}
		maskRoot.attrs = append(maskRoot.attrs, a)
	}
	// The clip's definitions, gradients and symbols still have to resolve.
	for _, k := range root.kids {
		if k.name.Local == "defs" || k.name.Local == "style" {
			maskRoot.kids = append(maskRoot.kids, k)
		}
	}
	if tr := attr(target, "transform"); tr != "" {
		g := &elem{name: xml.Name{Local: "g"}, kids: shapes}
		g.attrs = append(g.attrs, xmlAttr("transform", tr))
		maskRoot.kids = append(maskRoot.kids, g)
	} else {
		maskRoot.kids = append(maskRoot.kids, shapes...)
	}
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	if err := emitElem(enc, maskRoot, collectCSS(root), 0, true); err != nil {
		return nil
	}
	if err := enc.Flush(); err != nil {
		return nil
	}
	return out.Bytes()
}

func isDrawable(e *elem) bool {
	switch e.name.Local {
	case "defs", "style", "title", "desc", "metadata", "script", "clipPath", "mask", "symbol":
		return false
	}
	return true
}

func clipURLID(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "url(") || !strings.HasSuffix(v, ")") {
		return ""
	}
	v = strings.TrimSpace(v[4 : len(v)-1])
	return strings.TrimPrefix(v, "#")
}

func findElement(root *elem, id string) *elem {
	var found *elem
	walkPaths(root, nil, func(e *elem, _ []int) {
		if found == nil && attr(e, "id") == id {
			found = e
		}
	})
	return found
}

func maskShape(e *elem) *elem {
	out := cloneElem(e)
	out.attrs = out.attrs[:0]
	for _, a := range e.attrs {
		switch a.Name.Local {
		case "style", "class", "clip-path", "mask", "filter", "opacity", "fill-opacity", "stroke-opacity":
			continue
		}
		out.attrs = append(out.attrs, a)
	}
	out.attrs = append(out.attrs, xmlAttr("fill", "#ffffff"), xmlAttr("stroke", "none"))
	return out
}

func cloneElem(e *elem) *elem {
	out := &elem{name: e.name, text: e.text}
	out.attrs = append(out.attrs, e.attrs...)
	for _, k := range e.kids {
		out.kids = append(out.kids, cloneElem(k))
	}
	return out
}

// luminanceMaskSource builds a document for a <mask>. It is the same
// whole-root approximation as a clip path: the mask's own paint is rendered and
// its luminance (or alpha) multiplies the picture.
func luminanceMaskSource(root *elem) ([]byte, bool) {
	if root == nil || root.name.Local != "svg" {
		return nil, false
	}
	target := root
	var drawable []*elem
	for _, k := range root.kids {
		if isDrawable(k) {
			drawable = append(drawable, k)
		}
	}
	ref := attr(target, "mask")
	if ref == "" {
		if len(drawable) != 1 {
			return nil, false
		}
		target = drawable[0]
		ref = attr(target, "mask")
	}
	if ref == "" {
		return nil, false
	}
	def := findElement(root, clipURLID(ref))
	if def == nil || def.name.Local != "mask" {
		return nil, false
	}
	if strings.TrimSpace(attr(def, "maskUnits")) == "objectBoundingBox" ||
		strings.TrimSpace(attr(def, "maskContentUnits")) == "objectBoundingBox" {
		return nil, false
	}
	var shapes []*elem
	for _, k := range def.kids {
		if k.name.Local == "text" || k.name.Local == "image" {
			return nil, false
		}
		shapes = append(shapes, cloneElem(k))
	}
	if len(shapes) == 0 {
		return nil, false
	}
	maskRoot := &elem{name: xml.Name{Local: "svg"}}
	for _, a := range root.attrs {
		switch a.Name.Local {
		case "clip-path", "mask", "style":
			continue
		}
		maskRoot.attrs = append(maskRoot.attrs, a)
	}
	for _, k := range root.kids {
		if k.name.Local == "defs" || k.name.Local == "style" {
			maskRoot.kids = append(maskRoot.kids, k)
		}
	}
	if tr := attr(target, "transform"); tr != "" {
		g := &elem{name: xml.Name{Local: "g"}, kids: shapes}
		g.attrs = append(g.attrs, xmlAttr("transform", tr))
		maskRoot.kids = append(maskRoot.kids, g)
	} else {
		maskRoot.kids = append(maskRoot.kids, shapes...)
	}
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	if err := emitElem(enc, maskRoot, collectCSS(root), 0, true); err != nil {
		return nil, false
	}
	if err := enc.Flush(); err != nil {
		return nil, false
	}
	alpha := strings.TrimSpace(attr(def, "mask-type")) == "alpha"
	return out.Bytes(), !alpha
}
