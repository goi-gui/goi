package ui

import (
	"fmt"
	"io/fs"
	"path"

	"github.com/ksckaan1/goui/internal/img"
	"github.com/ksckaan1/goui/internal/tree"
)

// loadImages decodes every Image element in a freshly built tree. Decoding is
// cached by path, so a hot reload of a design with photos does not decode them
// again, and a failed file is reported once instead of every frame.
func (a *App) loadImages(tr *tree.Tree) {
	if tr == nil || a.assets == nil {
		return
	}
	for _, n := range tr.All() {
		im, ok := n.(*tree.Image)
		if !ok {
			continue
		}
		src := im.Src.Get()
		if src == "" {
			continue
		}
		if a.images == nil {
			a.images = map[string]*img.Data{}
		}
		d, cached := a.images[src]
		if !cached {
			b, err := fs.ReadFile(a.assets, path.Clean(src))
			if err != nil {
				im.LoadErr = err
				continue
			}
			if d, err = img.Decode(b); err != nil {
				im.LoadErr = fmt.Errorf("%s: %w", src, err)
				continue
			}
			a.images[src] = d
		}
		im.Data = d
	}
}
