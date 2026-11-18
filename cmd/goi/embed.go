//go:build !js && !wasip1

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const embedSource = `package main

import (
	"embed"
	"io/fs"

	"github.com/goi-gui/goi/ui"
)

//go:embed all:assets
var goiEmbeddedAssets embed.FS

func init() {
	design, err := fs.Sub(goiEmbeddedAssets, "assets")
	if err != nil {
		panic(err)
	}
	ui.RegisterAssets(design)
}
`

// stageBuild copies the entrypoint package and the design into a temporary
// package inside the module. //go:embed cannot reach a sibling ui directory, so
// the copy is what lets goi build ship the design inside the binary.
func stageBuild(moduleDir, entrypoint, uiDir string) (string, error) {
	entrySrc := resolvePath(moduleDir, entrypoint)
	uiSrc := resolvePath(moduleDir, uiDir)
	if sameDir(entrySrc, moduleDir) || sameDir(uiSrc, moduleDir) {
		return "", errors.New("goi build needs the entrypoint and ui directories below the project root")
	}
	buildDir, err := os.MkdirTemp(moduleDir, "goi-build-")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		os.RemoveAll(buildDir)
		return "", err
	}
	if err := copyTree(entrySrc, buildDir); err != nil {
		return fail(err)
	}
	if err := copyTree(uiSrc, filepath.Join(buildDir, "assets")); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "goi_embed.go"), []byte(embedSource), 0o644); err != nil {
		return fail(err)
	}
	return buildDir, nil
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

func sameDir(a, b string) bool {
	aa, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
