package csscolor

import (
	"strconv"
	"strings"
)

// GradientKind names the shape a gradient fills with.
type GradientKind uint8

// Gradient kinds.
const (
	Linear GradientKind = iota
	Radial
)

// Stop is one colour along a gradient, at a fraction 0..1.
type Stop struct {
	At    float64
	Color uint32
}

// Gradient is a CSS gradient value: a linear one along an angle, or a radial one
// out from the centre.
type Gradient struct {
	Kind GradientKind
	// Angle is the linear direction in degrees, 0 pointing up, growing
	// clockwise, the way CSS `90deg` points right.
	Angle float64
	Stops []Stop
}

// IsGradient reports whether s looks like a gradient function, so a caller can
// tell "not a colour" from "a gradient this build cannot draw".
func IsGradient(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "linear-gradient(") || strings.HasPrefix(s, "radial-gradient(")
}

// ParseGradient reads `linear-gradient(90deg, red, blue 60%)` or
// `radial-gradient(circle, rgba(…) 0%, rgba(…) 100%)`. Direction keywords are
// understood (`to right`, `to bottom left`); a size keyword such as `circle` or
// `closest-side` is accepted and ignored because the element's own box is the
// circle.
func ParseGradient(s string) (*Gradient, bool) {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	var g Gradient
	switch {
	case strings.HasPrefix(lower, "linear-gradient("):
		g.Kind = Linear
	case strings.HasPrefix(lower, "radial-gradient("):
		g.Kind = Radial
	default:
		return nil, false
	}
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return nil, false
	}
	inner := s[open+1 : len(s)-1]
	parts := splitTopLevel(inner)
	if len(parts) < 2 {
		return nil, false
	}
	idx := 0
	if g.Kind == Linear {
		if a, ok := parseLinearNoun(parts[0]); ok {
			g.Angle = a
			idx = 1
		}
	} else if !looksLikeColor(parts[0]) {
		idx = 1
	}
	var stops []Stop
	for _, p := range parts[idx:] {
		st, ok := parseStop(p)
		if !ok {
			return nil, false
		}
		stops = append(stops, st)
	}
	if len(stops) < 2 {
		return nil, false
	}
	fillMissingStops(stops)
	g.Stops = stops
	return &g, true
}

// splitTopLevel splits on commas that are not inside parentheses, so the
// arguments of rgb() stay together.
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

// parseLinearNoun reads a direction: `90deg`, `0.25turn`, `to right`, `to top
// left`. It reports false when the part is a colour, which means the default
// direction (`to bottom`).
func parseLinearNoun(s string) (float64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "to ") {
		dir := strings.TrimSpace(strings.TrimPrefix(s, "to "))
		switch dir {
		case "top":
			return 0, true
		case "top right", "right top":
			return 45, true
		case "right":
			return 90, true
		case "bottom right", "right bottom":
			return 135, true
		case "bottom":
			return 180, true
		case "bottom left", "left bottom":
			return 225, true
		case "left":
			return 270, true
		case "top left", "left top":
			return 315, true
		}
		return 0, false
	}
	if strings.HasSuffix(s, "deg") {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(s, "deg"), 64); err == nil {
			return v, true
		}
		return 0, false
	}
	if strings.HasSuffix(s, "turn") {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(s, "turn"), 64); err == nil {
			return v * 360, true
		}
		return 0, false
	}
	if strings.HasSuffix(s, "rad") {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(s, "rad"), 64); err == nil {
			return v * 180 / 3.141592653589793, true
		}
		return 0, false
	}
	return 0, false
}

// parseStop reads one stop: a colour, optionally followed by one or two
// positions (a percentage or a 0..1 number).
func parseStop(s string) (Stop, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Stop{}, false
	}
	// The colour may be a function, so find where its parentheses close.
	end := len(s)
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i+1 < len(s) && s[i+1] == ' ' {
				end = i + 1
				i = len(s)
			}
		}
	}
	colorText := strings.TrimSpace(s[:end])
	rest := strings.TrimSpace(s[end:])
	// `red` alone has no function, so the split above leaves it whole.
	if end == len(s) {
		colorText, rest = colorAndRest(s)
	}
	c, ok := Parse(colorText)
	if !ok {
		return Stop{}, false
	}
	st := Stop{Color: c, At: -1}
	if rest != "" {
		fields := strings.Fields(rest)
		if f, ok := parsePosition(fields[0]); ok {
			st.At = f
		}
	}
	return st, true
}

// colorAndRest splits "red 40%" without a function in it.
func colorAndRest(s string) (string, string) {
	fields := strings.Fields(s)
	for i := len(fields); i > 0; i-- {
		cand := strings.Join(fields[:i], " ")
		if _, ok := Parse(cand); ok {
			return cand, strings.Join(fields[i:], " ")
		}
	}
	return s, ""
}

// looksLikeColor reports whether a gradient argument is already a colour, which
// means the direction was omitted.
func looksLikeColor(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "rgb") || strings.HasPrefix(s, "hsl") || strings.HasPrefix(s, "#") {
		return true
	}
	if _, ok := names[strings.ToLower(s)]; ok {
		return true
	}
	// `red 40%` is a stop, not a direction.
	_, rest := colorAndRest(s)
	return rest != ""
}

func parsePosition(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	percent := strings.HasSuffix(s, "%")
	s = strings.TrimSuffix(s, "%")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if percent {
		v /= 100
	}
	return v, true
}

// fillMissingStops gives every stop a position the way CSS does: a missing one
// is spread evenly between its neighbours, and the ends default to 0 and 1.
func fillMissingStops(stops []Stop) {
	if stops[0].At < 0 {
		stops[0].At = 0
	}
	if stops[len(stops)-1].At < 0 {
		stops[len(stops)-1].At = 1
	}
	i := 0
	for i < len(stops) {
		if stops[i].At >= 0 {
			i++
			continue
		}
		j := i
		for j < len(stops) && stops[j].At < 0 {
			j++
		}
		if j >= len(stops) {
			break
		}
		from, to := stops[i-1].At, stops[j].At
		n := float64(j - i + 1)
		for k := i; k < j; k++ {
			stops[k].At = from + (to-from)*float64(k-i+1)/n
		}
		i = j
	}
	// Positions are fractions; a stop past the end clamps like CSS.
	for k := range stops {
		if stops[k].At < 0 {
			stops[k].At = 0
		}
		if stops[k].At > 1 {
			stops[k].At = 1
		}
	}
}
