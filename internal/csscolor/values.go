package csscolor

import (
	"math"
	"strconv"
	"strings"
)

// ValueStop is one stop of a scalar gradient: Value is the radius in dp and the
// position is a fraction (At) or a length (Len), like Stop.
type ValueStop struct {
	Value float64
	At    float64
	Len   float64
}

// ValueGradient is a gradient of numbers, the shape `backdropBlur` takes. It
// uses the same geometry as a CSS gradient, but every stop is a blur radius in
// dp instead of a colour.
type ValueGradient struct {
	Kind  GradientKind
	Angle float64
	Stops []ValueStop
}

// ParseValueGradient reads `linear-gradient(90deg, 0 0%, 6 60%, 0 100%)` or
// `radial-gradient(circle, 0 0, 6 100, 0 260)`. The first number of a stop is
// the radius; the optional second one is its position, as a percentage, a `dp`
// length or a bare length above 1.
func ParseValueGradient(s string) (*ValueGradient, bool) {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	var g ValueGradient
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
	parts := splitTopLevel(s[open+1 : len(s)-1])
	if len(parts) < 2 {
		return nil, false
	}
	idx := 0
	if g.Kind == Linear {
		if a, ok := parseLinearNoun(parts[0]); ok {
			g.Angle = a
			idx = 1
		}
	} else if !startsValueStop(parts[0]) {
		idx = 1
	}
	if len(parts)-idx < 2 {
		return nil, false
	}
	for _, p := range parts[idx:] {
		st, ok := parseValueStop(p)
		if !ok {
			return nil, false
		}
		g.Stops = append(g.Stops, st)
	}
	return &g, true
}

// startsValueStop reports whether a gradient argument is a numeric stop, which
// means a radial gradient's size keyword was omitted.
func startsValueStop(s string) bool {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return false
	}
	_, err := strconv.ParseFloat(fields[0], 64)
	return err == nil
}

func parseValueStop(s string) (ValueStop, bool) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return ValueStop{}, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return ValueStop{}, false
	}
	st := ValueStop{Value: v, At: -1}
	if len(fields) > 1 {
		if at, ln, ok := parsePosition(fields[1]); ok {
			if ln > 0 {
				st.Len = ln
			} else {
				st.At = at
			}
		}
	}
	return st, true
}

// Max is the largest radius in the gradient, which the renderer blurs with; the
// stops become the alpha mask between the unblurred scene and that radius.
func (g *ValueGradient) Max() float64 {
	max := 0.0
	for _, st := range g.Stops {
		max = math.Max(max, st.Value)
	}
	return max
}

// Resolve converts dp stop positions to fractions of the gradient line and
// fills the stops that left their position out.
func (g *ValueGradient) Resolve(w, h float64) []ValueStop {
	out := append([]ValueStop(nil), g.Stops...)
	if len(out) == 0 {
		return out
	}
	axis := gradientAxis(g.Kind, g.Angle, w, h)
	for i := range out {
		if out[i].Len > 0 {
			out[i].At = out[i].Len / axis
		}
	}
	fillMissingValueStops(out)
	return out
}

// fillMissingValueStops spreads a missing position between its neighbours, the
// way a CSS gradient does, and clamps the ends to the gradient line.
func fillMissingValueStops(stops []ValueStop) {
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
	for k := range stops {
		if stops[k].At < 0 {
			stops[k].At = 0
		}
		if stops[k].At > 1 {
			stops[k].At = 1
		}
	}
}
