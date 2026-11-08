package svg

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// SMIL is what an animated SVG in the wild actually uses: `<animate>` on an
// attribute and `<animateTransform>` on a transform. This file reads that small
// subset, interpolates the attributes for a moment in time and hands the result
// to the normal rasterizer. It is not a SMIL engine: keyTimes, begin lists,
// additive animation and path morphing are ignored, which is why an unsupported
// file still draws its first frame instead of failing.
type anim struct {
	// path is the element the animation belongs to, as child indexes from the
	// root, so the same tree can be walked again per frame.
	path []int
	attr string
	// values are the keyframe strings; keyTimes their fractions, or nil for
	// even spacing.
	values    []string
	keyTimes  []float64
	dur       time.Duration
	begin     time.Duration
	repeat    int // 0 loops forever
	transform string
}

// elem is one node of the parsed document, kept so an animation can be applied
// without re-parsing the file for every frame.
type elem struct {
	name  xml.Name
	attrs []xml.Attr
	kids  []*elem
	text  string
	// anims are the animations declared inside this element.
	anims []*anim
}

// parseTree reads a document into elems. Comments and processing instructions
// are dropped; both are irrelevant to the rasterizer.
func parseTree(b []byte) (*elem, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	var root *elem
	var stack []*elem
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("svg: parse: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			e := &elem{name: t.Name, attrs: t.Attr}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.kids = append(p.kids, e)
			} else if root == nil {
				root = e
			}
			stack = append(stack, e)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("svg: parse: no root element")
	}
	return root, nil
}

// walkPaths numbers every element by its position in its parent.
func walkPaths(e *elem, path []int, fn func(*elem, []int)) {
	fn(e, path)
	for i, k := range e.kids {
		walkPaths(k, append(path[:len(path):len(path)], i), fn)
	}
}

// collectAnims turns every `<animate>` and `<animateTransform>` in the tree into
// an anim, and returns the longest one.
func collectAnims(root *elem) ([]*anim, time.Duration) {
	var out []*anim
	var longest time.Duration
	walkPaths(root, nil, func(e *elem, path []int) {
		for _, k := range e.kids {
			a := animationFor(k, path)
			if a == nil {
				continue
			}
			e.anims = append(e.anims, a)
			out = append(out, a)
			if d := a.begin + a.dur; d > longest {
				longest = d
			}
		}
	})
	return out, longest
}

// animationFor reads one animation element. It returns nil for a child that is
// not an animation, or one this engine does not implement.
func animationFor(e *elem, path []int) *anim {
	var attr string
	var a *anim
	switch e.name.Local {
	case "animate":
		a = &anim{}
	case "animateTransform":
		a = &anim{transform: attrOf(e, "type")}
		if a.transform == "" {
			a.transform = "translate"
		}
		attr = "transform"
	default:
		return nil
	}
	if attr == "" {
		attr = attrOf(e, "attributeName")
	}
	if attr == "" {
		return nil
	}
	a.attr = attr
	a.path = append([]int(nil), path...)

	if v := attrOf(e, "values"); v != "" {
		for _, part := range strings.Split(v, ";") {
			if part = strings.TrimSpace(part); part != "" {
				a.values = append(a.values, part)
			}
		}
	}
	if len(a.values) < 2 {
		from, to := attrOf(e, "from"), attrOf(e, "to")
		if from == "" || to == "" {
			return nil // by= and to= alone need the base value; not supported
		}
		a.values = []string{from, to}
	}
	if kt := attrOf(e, "keyTimes"); kt != "" {
		for _, part := range strings.Split(kt, ";") {
			f, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
			if err != nil {
				a.keyTimes = nil
				break
			}
			a.keyTimes = append(a.keyTimes, f)
		}
		if len(a.keyTimes) != len(a.values) {
			a.keyTimes = nil
		}
	}
	a.dur = parseClock(attrOf(e, "dur"))
	if a.dur <= 0 {
		return nil
	}
	a.begin = parseClock(attrOf(e, "begin"))
	switch r := attrOf(e, "repeatCount"); {
	case r == "indefinite" || r == "":
		a.repeat = 0
	default:
		// Any finite count holds its last frame, like fill="freeze", which is
		// what an icon spinner wants when it is not looping.
		a.repeat = 1
	}
	return a
}

func attrOf(e *elem, name string) string {
	for _, a := range e.attrs {
		if a.Name.Local == name {
			return strings.TrimSpace(a.Value)
		}
	}
	return ""
}

// parseClock reads a SMIL time: "1.5s", "500ms", "0.5min", or bare seconds.
func parseClock(s string) time.Duration {
	if s == "" {
		return 0
	}
	unit := time.Second
	switch {
	case strings.HasSuffix(s, "ms"):
		unit, s = time.Millisecond, strings.TrimSuffix(s, "ms")
	case strings.HasSuffix(s, "min"):
		unit, s = time.Minute, strings.TrimSuffix(s, "min")
	case strings.HasSuffix(s, "h"):
		unit, s = time.Hour, strings.TrimSuffix(s, "h")
	case strings.HasSuffix(s, "s"):
		s = strings.TrimSuffix(s, "s")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return time.Duration(f * float64(unit))
}

// valueAt returns the attribute value an animation asks for at elapsed. The
// second result is false before the animation starts.
func (a *anim) valueAt(elapsed time.Duration, loop bool) (string, bool) {
	if elapsed < a.begin {
		return "", false
	}
	t := elapsed - a.begin
	span := a.dur
	if loop && a.repeat == 0 || loop && a.repeat > 1 {
		t %= span
	} else if t >= span {
		t = span
	}
	frac := 0.0
	if span > 0 {
		frac = float64(t) / float64(span)
	}
	return a.interpolate(frac), true
}

// interpolate blends the keyframe values at frac, walking the segment it lands
// in.
func (a *anim) interpolate(frac float64) string {
	n := len(a.values)
	if n < 2 {
		return ""
	}
	lo, hi := 0.0, 1.0
	idx := 0
	for i := 0; i < n-1; i++ {
		lo, hi = float64(i)/float64(n-1), float64(i+1)/float64(n-1)
		if a.keyTimes != nil {
			lo, hi = a.keyTimes[i], a.keyTimes[i+1]
		}
		if frac <= hi || i == n-2 {
			idx = i
			break
		}
	}
	local := 0.0
	if hi > lo {
		local = (frac - lo) / (hi - lo)
	}
	local = math.Max(0, math.Min(1, local))
	out, ok := blend(a.values[idx], a.values[idx+1], local)
	if !ok {
		if local < 0.5 {
			return a.values[idx]
		}
		return a.values[idx+1]
	}
	if a.transform != "" {
		return a.transform + "(" + out + ")"
	}
	return out
}

// blend interpolates two SMIL values of the same shape: numbers, number lists
// (a transform's arguments) or colours.
func blend(from, to string, t float64) (string, bool) {
	if c0, ok := parseColor(from); ok {
		c1, ok := parseColor(to)
		if !ok {
			return "", false
		}
		return formatColor(mixColor(c0, c1, t)), true
	}
	f0, ok0 := numbers(from)
	f1, ok1 := numbers(to)
	if !ok0 || !ok1 || len(f0) != len(f1) {
		return "", false
	}
	parts := make([]string, len(f0))
	for i := range f0 {
		v := f0[i] + (f1[i]-f0[i])*t
		parts[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(parts, " "), true
}

// numbers splits "0 12 12" or "0,12,12" into floats.
func numbers(s string) ([]float64, bool) {
	fields := strings.FieldsFunc(strings.TrimSpace(s), func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	})
	out := make([]float64, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, false
		}
		out = append(out, v)
	}
	return out, len(out) > 0
}

// parseColor reads the colour forms an SVG animation carries: #rgb, #rrggbb,
// rgb(r,g,b) and rgba(...).
func parseColor(s string) (uint32, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "#") {
		h := strings.TrimPrefix(s, "#")
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) != 6 {
			return 0, false
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return 0, false
		}
		return uint32(v)<<8 | 0xFF, true
	}
	if strings.HasPrefix(s, "rgb(") || strings.HasPrefix(s, "rgba(") {
		inner := s[strings.IndexByte(s, '(')+1 : len(s)-1]
		n, ok := numbers(strings.ReplaceAll(inner, "/", " "))
		if !ok || len(n) < 3 {
			return 0, false
		}
		a := 255.0
		if len(n) > 3 {
			a = n[3]
		}
		return uint32(clamp8(n[0]))<<24 | uint32(clamp8(n[1]))<<16 | uint32(clamp8(n[2]))<<8 | uint32(clamp8(a)), true
	}
	return 0, false
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func mixColor(a, b uint32, t float64) uint32 {
	ch := func(shift uint) uint32 {
		x := float64((a >> shift) & 0xFF)
		y := float64((b >> shift) & 0xFF)
		return uint32(x + (y-x)*t + 0.5)
	}
	return ch(24)<<24 | ch(16)<<16 | ch(8)<<8 | ch(0)
}

func formatColor(c uint32) string {
	return fmt.Sprintf("#%02x%02x%02x", (c>>24)&0xFF, (c>>16)&0xFF, (c>>8)&0xFF)
}

// renderAt writes the document with every animation applied at elapsed.
func (s *SVG) renderAt(elapsed time.Duration, loop bool) []byte {
	if s.tree == nil || len(s.anims) == 0 {
		return s.src
	}
	var b bytes.Buffer
	enc := xml.NewEncoder(&b)
	if err := emitElem(enc, s.tree, elapsed, loop); err != nil {
		return s.src
	}
	if err := enc.Flush(); err != nil {
		return s.src
	}
	return b.Bytes()
}

// emitElem writes one element, replacing the attributes its animations override
// and dropping the animation elements themselves: oksvg would ignore them.
func emitElem(enc *xml.Encoder, e *elem, elapsed time.Duration, loop bool) error {
	over := map[string]string{}
	for _, a := range e.anims {
		if v, ok := a.valueAt(elapsed, loop); ok {
			over[a.attr] = v
		}
	}
	start := xml.StartElement{Name: e.name}
	for _, at := range e.attrs {
		if v, ok := over[at.Name.Local]; ok {
			start.Attr = append(start.Attr, xml.Attr{Name: at.Name, Value: v})
			delete(over, at.Name.Local)
			continue
		}
		start.Attr = append(start.Attr, at)
	}
	// An attribute the base element does not carry at all still gets its
	// animated value.
	for name, v := range over {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: name}, Value: v})
	}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if e.text != "" {
		if err := enc.EncodeToken(xml.CharData(e.text)); err != nil {
			return err
		}
	}
	for _, k := range e.kids {
		if k.name.Local == "animate" || k.name.Local == "animateTransform" {
			continue
		}
		if err := emitElem(enc, k, elapsed, loop); err != nil {
			return err
		}
	}
	return enc.EncodeToken(start.End())
}
