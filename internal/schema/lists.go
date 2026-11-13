package schema

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/csscolor"
)

// NumberList reads a comma-separated list of numbers, which is how a design
// writes the values of a chart series: `values: "12, 18, 9"`. Empty entries are
// skipped, so a trailing comma is harmless. The checker and the builder share
// it, so what the file accepts is what the chart draws.
func NumberList(s string) ([]float64, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", part)
		}
		out = append(out, v)
	}
	return out, nil
}

// LabelList reads a comma-separated list of labels: `categories: "Jan, Feb"`.
func LabelList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, len(parts))
	for i, part := range parts {
		// An empty label keeps its place, because a point without a name falls
		// back to its number; dropping it would shift every label after it.
		out[i] = strings.TrimSpace(part)
	}
	return out
}

// ColorList reads a comma-separated list of colours, which is how a design
// colours each slice or point: `colors: "primary, tertiary, #c62828"`. Each item
// is a CSS colour or a theme role, the same two forms a colour property takes.
// Literals come back in colors, roles in roles at the same index (empty where
// the item is a literal), so the painter can resolve a role against the scheme
// it draws with.
func ColorList(s string) (colors []uint32, roles []string, err error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil, nil
	}
	parts := strings.Split(s, ",")
	colors = make([]uint32, len(parts))
	roles = make([]string, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, nil, fmt.Errorf("entry %d is empty", i+1)
		}
		if rgba, ok := csscolor.Parse(part); ok {
			colors[i] = rgba
			continue
		}
		// A word that is not a colour can only be a role. A malformed literal
		// is caught here rather than drawn as the scheme's default.
		if strings.HasPrefix(part, "#") || strings.ContainsAny(part, "()") {
			return nil, nil, fmt.Errorf("%q is not a colour", part)
		}
		roles[i] = part
	}
	return colors, roles, nil
}
