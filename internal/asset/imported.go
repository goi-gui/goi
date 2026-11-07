package asset

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
)

// IsImported reports whether a sibling .goi file imports path. A fragment that
// declares neither props nor a slot has no marker of its own, so a tool opening
// it alone would treat it as an entry point and demand a Window root. The file
// is a fragment in practice when something in its folder calls it.
//
// shortcut: only siblings in the same folder are scanned, because imports that
// climb out of the folder are rare in a design; walk up if shared fragments
// become common.
func IsImported(path string) bool {
	self := filepath.Clean(path)
	dir := filepath.Dir(self)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".goi") {
			continue
		}
		sibling := filepath.Join(dir, name)
		if filepath.Clean(sibling) == self {
			continue
		}
		src, err := os.ReadFile(sibling)
		if err != nil {
			continue
		}
		f, diag := syntax.Parse(src)
		if !diag.Empty() {
			continue
		}
		for _, imp := range f.Imports {
			if filepath.Clean(filepath.Join(dir, filepath.FromSlash(imp.Path))) == self {
				return true
			}
		}
	}
	return false
}
