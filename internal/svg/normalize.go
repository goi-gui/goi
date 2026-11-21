//go:build all || svg

package svg

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"
)

func xmlAttr(name, value string) xml.Attr {
	return xml.Attr{Name: xml.Name{Local: name}, Value: value}
}

// urlOnly keeps the gradient reference when a style writes a fallback colour
// after it, as Inkscape does: fill:url(#g) rgb(0,0,0). oksvg reads that as an
// invalid colour and falls back to black.
func urlOnly(v string) string {
	if !strings.HasPrefix(strings.TrimSpace(v), "url(") {
		return v
	}
	if i := strings.IndexByte(v, ')'); i >= 0 {
		return strings.TrimSpace(v[:i+1])
	}
	return v
}

// inlineStyle returns the declarations in an element's style attribute. oksvg
// understands the fill property there but not the stop-color, stroke and
// opacity properties a design tool writes, so the caller folds these into
// presentation attributes instead.
func inlineStyle(e *elem) map[string]string {
	for _, a := range e.attrs {
		if a.Name.Local == "style" {
			return parseDecls(a.Value)
		}
	}
	return nil
}

// attr returns the first attribute with the given local name. xlink:href and
// href are both stored as Local "href".
func attr(e *elem, name string) string {
	for _, a := range e.attrs {
		if a.Name.Local == name {
			return strings.TrimSpace(a.Value)
		}
	}
	return ""
}

func hasAttr(e *elem, name string) bool {
	for _, a := range e.attrs {
		if a.Name.Local == name {
			return true
		}
	}
	return false
}

func setAttr(e *elem, name, value string) {
	for i := range e.attrs {
		if e.attrs[i].Name.Local == name {
			e.attrs[i].Value = value
			return
		}
	}
	e.attrs = append(e.attrs, xmlAttr(name, value))
}

// resolveGradientRefs copies the stops and missing attributes a gradient asks
// for through xlink:href. oksvg resolves a direct gradient but not one that
// inherits its stops from another gradient, which is how Inkscape exports them.
func resolveGradientRefs(root *elem) {
	byID := make(map[string]*elem)
	walkPaths(root, nil, func(e *elem, _ []int) {
		if id := attr(e, "id"); id != "" {
			byID[id] = e
		}
	})
	var resolve func(e *elem, seen map[*elem]bool)
	resolve = func(e *elem, seen map[*elem]bool) {
		if seen[e] {
			return
		}
		seen[e] = true
		href := attr(e, "href")
		if href == "" {
			return
		}
		target := byID[strings.TrimPrefix(href, "#")]
		if target == nil || target == e {
			return
		}
		if target.name.Local != "linearGradient" && target.name.Local != "radialGradient" {
			return
		}
		resolve(target, seen)
		for _, a := range target.attrs {
			switch a.Name.Local {
			case "id", "href":
				continue
			}
			if !hasAttr(e, a.Name.Local) {
				e.attrs = append(e.attrs, a)
			}
		}
		if !hasChild(e, "stop") {
			for _, k := range target.kids {
				if k.name.Local == "stop" {
					e.kids = append(e.kids, k)
				}
			}
		}
		// The inherited content is now local, so oksvg does not need the link.
		filtered := e.attrs[:0]
		for _, a := range e.attrs {
			if a.Name.Local != "href" {
				filtered = append(filtered, a)
			}
		}
		e.attrs = filtered
	}
	for _, e := range allGradients(root) {
		resolve(e, map[*elem]bool{})
	}
}

func resolvePatternRefs(root *elem) {
	byID := make(map[string]*elem)
	walkPaths(root, nil, func(e *elem, _ []int) {
		if id := attr(e, "id"); id != "" {
			byID[id] = e
		}
	})
	var resolve func(e *elem, seen map[*elem]bool)
	resolve = func(e *elem, seen map[*elem]bool) {
		if seen[e] {
			return
		}
		seen[e] = true
		href := attr(e, "href")
		if href == "" {
			return
		}
		target := byID[strings.TrimPrefix(href, "#")]
		if target == nil || target.name.Local != "pattern" {
			return
		}
		resolve(target, seen)
		for _, a := range target.attrs {
			if a.Name.Local == "id" || a.Name.Local == "href" {
				continue
			}
			if !hasAttr(e, a.Name.Local) {
				e.attrs = append(e.attrs, a)
			}
		}
		if !hasChild(e, "rect") && !hasChild(e, "path") && !hasChild(e, "circle") &&
			!hasChild(e, "ellipse") && !hasChild(e, "polygon") && !hasChild(e, "polyline") &&
			!hasChild(e, "line") && !hasChild(e, "use") {
			e.kids = append(e.kids, target.kids...)
		}
		filtered := e.attrs[:0]
		for _, a := range e.attrs {
			if a.Name.Local != "href" {
				filtered = append(filtered, a)
			}
		}
		e.attrs = filtered
	}
	walkPaths(root, nil, func(e *elem, _ []int) {
		if e.name.Local == "pattern" {
			resolve(e, map[*elem]bool{})
		}
	})
}

func allGradients(root *elem) []*elem {
	var out []*elem
	walkPaths(root, nil, func(e *elem, _ []int) {
		if e.name.Local == "linearGradient" || e.name.Local == "radialGradient" {
			out = append(out, e)
		}
	})
	return out
}

func hasChild(e *elem, name string) bool {
	for _, k := range e.kids {
		if k.name.Local == name {
			return true
		}
	}
	return false
}

// normalizeTransform fixes a single-argument scale(), which oksvg parses as
// two zero arguments and then draws nothing. scale(2) is scale(2,2) in SVG.
func normalizeTransform(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); {
		j := strings.Index(v[i:], "scale(")
		if j < 0 {
			b.WriteString(v[i:])
			break
		}
		j += i
		b.WriteString(v[i:j])
		open := j + len("scale(")
		close := strings.IndexByte(v[open:], ')')
		if close < 0 {
			b.WriteString(v[j:])
			break
		}
		close += open
		arg := strings.TrimSpace(v[open:close])
		if arg != "" && !strings.Contains(arg, ",") && !strings.ContainsAny(arg, " \t\r\n") {
			b.WriteString("scale(")
			b.WriteString(arg)
			b.WriteString(",")
			b.WriteString(arg)
			b.WriteString(")")
		} else {
			b.WriteString(v[j : close+1])
		}
		i = close + 1
	}
	return b.String()
}

// lengthPx reads an SVG length for the intrinsic-size fallback. A percentage or
// an unknown unit returns false, because there is no viewport to resolve it
// against here.
func lengthPx(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasSuffix(v, "%") {
		return 0, false
	}
	i := len(v)
	for i > 0 && (v[i-1] < '0' || v[i-1] > '9') && v[i-1] != '.' {
		i--
	}
	num, err := strconv.ParseFloat(strings.TrimSpace(v[:i]), 64)
	if err != nil || num <= 0 {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(v[i:])) {
	case "", "px":
		return num, true
	case "pt":
		return num * 96 / 72, true
	case "pc":
		return num * 16, true
	case "mm":
		return num * 96 / 25.4, true
	case "cm":
		return num * 96 / 2.54, true
	case "in":
		return num * 96, true
	}
	return 0, false
}

// normalizeViewBox moves a negative viewBox origin to zero by wrapping the
// drawable children in a translate. oksvg's SetTarget applies ViewBox.X/Y twice
// when it scales, so a viewBox such as -70 -70 140 140 draws cropped; the
// wrapper makes the origin ordinary.
func viewBoxOf(e *elem) (x, y, w, h float64, ok bool) {
	fields := strings.FieldsFunc(attr(e, "viewBox"), func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(fields) != 4 {
		return 0, 0, 0, 0, false
	}
	vals := [4]float64{}
	for i, f := range fields {
		n, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return 0, 0, 0, 0, false
		}
		vals[i] = n
	}
	return vals[0], vals[1], vals[2], vals[3], true
}

func normalizeViewBox(root *elem) {
	if root == nil || root.name.Local != "svg" {
		return
	}
	vx, vy, vw, vh, ok := viewBoxOf(root)
	if !ok || (vx == 0 && vy == 0) {
		return
	}
	var keep, draw []*elem
	for _, k := range root.kids {
		switch k.name.Local {
		case "defs", "style", "title", "desc", "metadata":
			keep = append(keep, k)
		default:
			draw = append(draw, k)
		}
	}
	if len(draw) == 0 {
		return
	}
	wrapper := &elem{name: xml.Name{Local: "g"}, kids: draw}
	wrapper.attrs = append(wrapper.attrs, xmlAttr("transform",
		"translate("+strconv.FormatFloat(-vx, 'f', -1, 64)+","+
			strconv.FormatFloat(-vy, 'f', -1, 64)+")"))
	root.kids = append(keep, wrapper)
	setAttr(root, "viewBox", "0 0 "+strconv.FormatFloat(vw, 'f', -1, 64)+" "+strconv.FormatFloat(vh, 'f', -1, 64))
}

// flattenNestedSVG turns an inner <svg> into a <g> that maps its viewBox into
// the x/y/width/height viewport. oksvg ignores an inner <svg> element, so its
// contents otherwise draw at the outer scale and position.
func flattenNestedSVG(root *elem) {
	if root == nil {
		return
	}
	for i, k := range root.kids {
		if k.name.Local == "svg" {
			root.kids[i] = nestedSVGGroup(k)
		}
		flattenNestedSVG(root.kids[i])
	}
}

func nestedSVGGroup(e *elem) *elem {
	vx, vy, vw, vh, hasBox := viewBoxOf(e)
	x, _ := lengthPx(attr(e, "x"))
	y, _ := lengthPx(attr(e, "y"))
	w, wok := lengthPx(attr(e, "width"))
	h, hok := lengthPx(attr(e, "height"))
	if !wok {
		w = vw
	}
	if !hok {
		h = vh
	}
	var transform string
	if hasBox && vw > 0 && vh > 0 && w > 0 && h > 0 {
		sx, sy := w/vw, h/vh
		if !strings.Contains(attr(e, "preserveAspectRatio"), "none") {
			s := math.Min(sx, sy)
			sx, sy = s, s
		}
		ox, oy := x, y
		if !strings.Contains(attr(e, "preserveAspectRatio"), "none") {
			ox += (w - vw*sx) / 2
			oy += (h - vh*sy) / 2
		}
		transform = "translate(" + ftoa(ox) + "," + ftoa(oy) + ") scale(" + ftoa(sx) + "," + ftoa(sy) + ") translate(" + ftoa(-vx) + "," + ftoa(-vy) + ")"
	} else if x != 0 || y != 0 {
		transform = "translate(" + ftoa(x) + "," + ftoa(y) + ")"
	}
	if old := attr(e, "transform"); old != "" {
		transform = old + " " + transform
	}
	g := &elem{name: xml.Name{Local: "g"}, kids: e.kids}
	for _, a := range e.attrs {
		switch a.Name.Local {
		case "x", "y", "width", "height", "viewBox", "preserveAspectRatio", "transform":
			continue
		}
		g.attrs = append(g.attrs, a)
	}
	if transform != "" {
		g.attrs = append(g.attrs, xmlAttr("transform", transform))
	}
	return g
}

func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// expandSymbolUses replaces a <use> that points at a <symbol> with the symbol's
// content wrapped in a viewport. oksvg understands <use> for an ordinary group
// but ignores a symbol target, which is how icon sprites ship.
func expandSymbolUses(root *elem) {
	byID := make(map[string]*elem)
	walkPaths(root, nil, func(e *elem, _ []int) {
		if e.name.Local == "symbol" {
			if id := attr(e, "id"); id != "" {
				byID[id] = e
			}
		}
	})
	var walk func(e *elem)
	walk = func(e *elem) {
		for i, k := range e.kids {
			if k.name.Local == "use" {
				if sym := byID[strings.TrimPrefix(attr(k, "href"), "#")]; sym != nil {
					e.kids[i] = symbolGroup(k, sym)
					walk(e.kids[i])
					continue
				}
			}
			walk(k)
		}
	}
	walk(root)
}

func symbolGroup(use, sym *elem) *elem {
	svg := &elem{name: xml.Name{Local: "svg"}, kids: sym.kids}
	// The symbol's viewBox decides the internal coordinate system.
	for _, a := range sym.attrs {
		switch a.Name.Local {
		case "viewBox", "preserveAspectRatio":
			svg.attrs = append(svg.attrs, a)
		}
	}
	// The instance supplies the viewport and the inherited paint.
	for _, a := range use.attrs {
		switch a.Name.Local {
		case "href", "x", "y", "width", "height", "transform", "fill", "stroke",
			"opacity", "fill-opacity", "stroke-opacity", "class", "style", "clip-path":
			svg.attrs = append(svg.attrs, a)
		}
	}
	vw, vh := 0.0, 0.0
	if _, _, w, h, ok := viewBoxOf(sym); ok {
		vw, vh = w, h
	}
	if !hasAttr(svg, "width") && vw > 0 {
		setAttr(svg, "width", ftoa(vw))
	}
	if !hasAttr(svg, "height") && vh > 0 {
		setAttr(svg, "height", ftoa(vh))
	}
	return nestedSVGGroup(svg)
}

// rootSize reads the width and height attributes when the file has no viewBox.
// oksvg leaves ViewBox zero in that case, and a file such as the classic tiger
// then collapses to the 24px fallback. With one axis declared, the other is
// treated as the same square, which is what a viewer shows for a standalone
// document with no viewport.
func rootSize(root *elem) (float64, float64) {
	if root == nil {
		return 0, 0
	}
	w, wok := lengthPx(attr(root, "width"))
	h, hok := lengthPx(attr(root, "height"))
	switch {
	case wok && hok:
		return w, h
	case wok:
		return w, w
	case hok:
		return h, h
	}
	return 0, 0
}
