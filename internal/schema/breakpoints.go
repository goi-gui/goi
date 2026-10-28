package schema

// Breakpoint is one responsive tier: values assigned to it apply when the
// window is at least Min logical pixels wide.
type Breakpoint struct {
	Name string
	Min  float64
}

// BaseBreakpoint is the tier below the first breakpoint. It has no minimum.
const BaseBreakpoint = "base"

// Breakpoints are the responsive tiers, ascending by minimum width. They follow
// Tailwind's min-width scale so the numbers feel familiar.
var Breakpoints = []Breakpoint{
	{Name: "sm", Min: 640},
	{Name: "md", Min: 768},
	{Name: "lg", Min: 1024},
	{Name: "xl", Min: 1280},
}

// BreakpointMin returns the minimum width for a tier name. Base is 0.
func BreakpointMin(name string) (float64, bool) {
	if name == BaseBreakpoint {
		return 0, true
	}
	for _, b := range Breakpoints {
		if b.Name == name {
			return b.Min, true
		}
	}
	return 0, false
}

// BreakpointNames returns base plus every breakpoint, in ascending order, for
// diagnostics and suggestions.
func BreakpointNames() []string {
	out := make([]string, 0, len(Breakpoints)+1)
	out = append(out, BaseBreakpoint)
	for _, b := range Breakpoints {
		out = append(out, b.Name)
	}
	return out
}
