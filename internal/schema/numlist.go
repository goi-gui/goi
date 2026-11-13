package schema

import (
	"fmt"
	"strconv"
	"strings"
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
