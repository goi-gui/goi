package ui

import (
	"fmt"
	"path"
	"strings"
)

// PickPrefix is the src a picked file gets. A design reads it like any other
// asset: `src: picked[0]` from Go, or a value passed into the tree.
const PickPrefix = "goi://picked/"

// pickName is a picked file's src. The index keeps two files of the same name
// apart, and the last segment stays the file's own name so a src reads well in a
// diagnostic.
func pickName(i int, name string) string {
	return fmt.Sprintf("%s%d/%s", PickPrefix, i, path.Base(strings.ReplaceAll(name, "\\", "/")))
}

// isPickSrc reports whether src names a file the user picked this session.
func isPickSrc(src string) bool { return strings.HasPrefix(src, PickPrefix) }

// putPicked registers one picked file and returns its src.
func (a *App) putPicked(i int, name string, data []byte) string {
	src := pickName(i, name)
	if a.picked == nil {
		a.picked = map[string][]byte{}
	}
	a.picked[src] = data
	return src
}
