package paint

import (
	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// Helpers for benchmarks: build a tree from source and lay it out.

func buildTree(src []byte) (*tree.Tree, string) {
	tr, diag := tree.BuildSource(src, nil)
	if !diag.Empty() {
		return nil, diag.Error()
	}
	return tr, ""
}

func layoutTree(tr *tree.Tree, w, h float64) {
	layout.New(nil).Layout(tr.Root, w, h)
}
