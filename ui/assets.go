package ui

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goi-gui/goi/internal/asset"
	"github.com/goi-gui/goi/internal/i18n"
	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/syntax"
)

var (
	assetsMu     sync.RWMutex
	staticAssets fs.FS
)

// RegisterAssets supplies a design container for a build that embeds the files.
// goi build generates the call; an app does not need to make it. LoadWindow
// prefers this over the ui directory, which is what makes a built app
// self-contained and keeps it from watching files.
func RegisterAssets(fsys fs.FS) {
	assetsMu.Lock()
	staticAssets = fsys
	assetsMu.Unlock()
}

func registeredAssets() fs.FS {
	assetsMu.RLock()
	defer assetsMu.RUnlock()
	return staticAssets
}

// dirFS is a directory that also reports where it came from, so hot reload can
// watch it. LoadWindow builds one from the ui value in goi.yaml.
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
	root, ok := watchRoot(fsys)
	if !ok {
		return nil
	}
	base := filepath.FromSlash(root)
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

// watchDirs is the design root plus its i18n folder, so a catalog edit reloads
// translations like a .goi edit reloads the design. A missing folder is ignored
// by hotreload.Watch.
func watchDirs(root string) []string {
	return []string{root, filepath.Join(root, filepath.FromSlash(i18n.Dir))}
}

// watchRoot reports the directory a design container lives in, or false when it
// cannot be watched. os.DirFS hides its path, which is why LoadWindow wraps the
// ui directory in dirFS.
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
