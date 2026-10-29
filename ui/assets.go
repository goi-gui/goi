package ui

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ksckaan1/goui/internal/asset"
	"github.com/ksckaan1/goui/internal/schema"
	"github.com/ksckaan1/goui/internal/syntax"
)

// DirFS wraps a directory as an fs.FS and remembers its path, so hot reload can
// watch it. Use it instead of os.DirFS when you want live reloading.
func DirFS(dir string) fs.FS { return dirFS(dir) }

// dirFS is a directory that also reports where it came from.
type dirFS string

// Open implements fs.FS.
func (d dirFS) Open(name string) (fs.File, error) { return os.DirFS(string(d)).Open(name) }

// Dir returns the directory path, for the watcher.
func (d dirFS) Dir() string { return string(d) }

// loadBundle reads the design and returns the expanded bundle. The error carries
// diagnostics formatted against the file each problem came from.
func loadBundle(fsys fs.FS, name string) (*asset.Bundle, error) {
	b, diag := asset.Load(fsys, name)
	if !diag.Empty() {
		return nil, &loadError{diag: diag, sources: b.Sources, name: name}
	}
	if _, diag := schema.ValidateEntry(b.File); !diag.Empty() {
		return nil, &loadError{diag: diag, sources: b.Sources, name: name}
	}
	return b, nil
}

// loadError carries diagnostics so the caller can render them with snippets from
// every file that took part.
type loadError struct {
	diag    syntax.Diagnostics
	sources map[string][]byte
	name    string
}

func (e *loadError) Error() string {
	return e.diag.FormatFiles(e.name, e.sources[e.name], e.sources)
}

// sourceDirs lists the directories that hold the files of a bundle, so the
// watcher can cover all of them.
func sourceDirs(b *asset.Bundle, fsys fs.FS) []string {
	root, ok := fsys.(interface{ Dir() string })
	if !ok {
		return nil
	}
	base := filepath.FromSlash(root.Dir())
	seen := map[string]bool{}
	var out []string
	for _, name := range b.Files {
		dir := filepath.Join(base, filepath.FromSlash(path.Dir(name)))
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return out
}

// watchRoot reports the directory a design lives in, or "" when the FS cannot be
// watched. os.DirFS hides its path, which is why ui.DirFS exists.
func watchRoot(fsys fs.FS) (string, bool) {
	if d, ok := fsys.(interface{ Dir() string }); ok {
		return d.Dir(), true
	}
	return "", false
}

// cleanName normalises a path for fs.FS: slash-separated, no leading slash.
func cleanName(name string) string {
	return strings.TrimPrefix(path.Clean("/"+name), "/")
}
