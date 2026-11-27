//go:build !setup && !all

package ui

import (
	"fmt"

	"github.com/goi-gui/goi/internal/syntax"
)

// loadSetups keeps a core build free of the interpreter. A design that asks
// for setup needs the tag; silently treating its names as literals would hide
// the missing feature.
func loadSetups(f *syntax.File) (setupBuild, error) {
	if f != nil && len(f.Setups) > 0 {
		return setupBuild{}, fmt.Errorf("this design uses setup; build with -tags setup (or all)")
	}
	return setupBuild{}, nil
}
