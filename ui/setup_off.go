//go:build !setup && !all

package ui

import (
	"fmt"

	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

// loadSetups keeps a core build free of the interpreter. A design that asks
// for setup needs the tag; silently treating its names as literals would hide
// the missing feature.
func loadSetups(sts []syntax.Setup) (func(string) tree.SetupScope, error) {
	if len(sts) > 0 {
		return nil, fmt.Errorf("this design uses setup; build with -tags setup (or all)")
	}
	return nil, nil
}
