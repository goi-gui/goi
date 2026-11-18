package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// windowConfig is the part of goi.yaml (or ui.yaml) that chooses the design.
// The CLI reads its own extensions from the same goi.yaml.
type windowConfig struct {
	// UI is the list of directories holding .goi files. The first one is the
	// root; extra directories are searched for imports that are not there.
	UI []string `yaml:"ui"`
	// Entry is the design file in one of those directories.
	Entry string `yaml:"entry"`
}

// LoadWindowFromConfig loads the window named by goi.yaml (or ui.yaml) in the
// working directory. The optional ui list defaults to ["ui"] and entry to
// "Main.goi"; hot reload is on unless an option turns it off.
//
// ui.yaml, when present, overrides only the UI fields; goi.yaml keeps the CLI
// extensions. This lets a project keep build settings and design settings
// together, or separate them when a tool needs a smaller file.
func (a *App) LoadWindowFromConfig(opts ...Option) (*Window, error) {
	cfg, err := readWindowConfig(".")
	if err != nil {
		return nil, err
	}
	dirs := cfg.UI
	if len(dirs) == 0 {
		dirs = []string{"ui"}
	}
	entry := cfg.Entry
	if entry == "" {
		entry = "Main.goi"
	}
	var assets fs.FS
	if len(dirs) == 1 {
		assets = DirFS(dirs[0])
	} else {
		assets = dirsFS(dirs)
	}
	all := append([]Option{WithHotReload(true)}, opts...)
	return a.LoadWindow(assets, entry, all...)
}

// readWindowConfig merges goi.yaml and then ui.yaml, so a ui.yaml can keep the
// design entry separate from build tags. Missing files are fine; a malformed one
// is an error.
func readWindowConfig(dir string) (windowConfig, error) {
	var out windowConfig
	for _, name := range []string{"goi.yaml", "ui.yaml"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return out, err
		}
		var c windowConfig
		if err := yaml.Unmarshal(b, &c); err != nil {
			return out, fmt.Errorf("%s: %w", name, err)
		}
		if len(c.UI) > 0 {
			out.UI = c.UI
		}
		if c.Entry != "" {
			out.Entry = c.Entry
		}
	}
	return out, nil
}

// dirsFS is an overlay of design directories. Files are looked up in order, so
// the first directory wins; imports from any of them work as if they shared one
// tree.
type dirsFS []string

func (d dirsFS) Open(name string) (fs.File, error) {
	name = cleanName(name)
	for _, root := range d {
		f, err := os.DirFS(root).Open(name)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, fs.ErrNotExist
}

// Dir reports the first root, for callers that need one directory.
func (d dirsFS) Dir() string { return d[0] }

// Dirs reports every root, so hot reload can watch all of them.
func (d dirsFS) Dirs() []string { return []string(d) }
