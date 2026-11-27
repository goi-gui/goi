//go:build setup || all

package ui

import (
	"github.com/goi-gui/goi/internal/setup"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

// loadSetups evaluates the design's setup blocks for this build.
func loadSetups(sts []syntax.Setup) (func(string) tree.SetupScope, error) {
	r, err := setup.New(sts)
	if err != nil {
		return nil, err
	}
	return r.For, nil
}
