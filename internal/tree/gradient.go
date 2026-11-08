package tree

import "github.com/goi-gui/goi/internal/csscolor"

// Gradient is a CSS gradient value, as `background: linear-gradient(...)` writes
// it. It is an alias so paint and the tree share one type.
type Gradient = csscolor.Gradient

// GradientStop is one colour of a gradient.
type GradientStop = csscolor.Stop

// Gradient kinds, re-exported so a caller never imports the parser.
const (
	LinearKind = csscolor.Linear
	RadialKind = csscolor.Radial
)

// ParseGradient reads a CSS gradient function.
func ParseGradient(s string) (*Gradient, bool) { return csscolor.ParseGradient(s) }
