//go:build setup || all

package ui

import (
	"github.com/goi-gui/goi/internal/setup"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
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
	return setupBuild{
		hook:     func(src, scope string) tree.SetupScope { return r.For(src, scope) },
		bind:     r.Bind,
		err:      r.Err,
		instance: r.Note,
		visible:  r.SetVisible,
		start:    r.Start,
		stop:     r.Unmount,
	}, nil
}
