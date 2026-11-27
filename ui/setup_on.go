//go:build setup || all

package ui

import (
	"github.com/goi-gui/goi/internal/setup"
	"github.com/goi-gui/goi/internal/syntax"
)

// loadSetups evaluates the design's setup blocks for this build.
func loadSetups(f *syntax.File) (setupBuild, error) {
	if f == nil {
		return setupBuild{}, nil
	}
	r, err := setup.New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		return setupBuild{}, err
	}
	return setupBuild{hook: r.For, bind: r.Bind}, nil
}
