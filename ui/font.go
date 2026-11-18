package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/goi-gui/goi/internal/render"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// LoadFont registers an app font under name, which is the family a design writes
// as `fontFamily`. path is resolved in the same assets directory LoadWindow
// uses, so a file at assets/fonts/Nunito.ttf is "fonts/Nunito.ttf":
//
//	if err := app.LoadFont("Nunito", "fonts/Nunito.ttf"); err != nil {
//		return err
//	}
//
//	// Window { fontFamily: "Nunito" }            // every text in the window
//	// Text { text: "Hi" fontFamily: "Nunito" }  // one element only
//
// One file serves every weight: a single face cannot vary, so a bold run draws
// the same outlines as a regular one. A family registered after the window is
// up takes effect on the next frame.
func (a *App) LoadFont(name, path string) error {
	if name == "" {
		return fmt.Errorf("ui: LoadFont needs a family name")
	}
	if path == "" {
		return fmt.Errorf("ui: LoadFont(%q) needs a path", name)
	}
	assets := a.assets
	if assets == nil {
		assets = registeredAssets()
	}
	if assets == nil {
		dir, err := designDir()
		if err != nil {
			return err
		}
		assets = dirFS(dir)
	}
	data, err := fs.ReadFile(assets, cleanName(filepath.ToSlash(path)))
	if err != nil {
		return fmt.Errorf("ui: LoadFont(%q): %w", name, err)
	}
	if a.fonts == nil {
		a.fonts = make(map[string][]byte, 1)
	}
	a.fonts[name] = data
	// A window may already be up, so write through to its library and repaint;
	// a window created later picks the font up from a.fonts.
	if lib := a.textLibrary(); lib != nil {
		if err := lib.RegisterFamily(name, data); err != nil {
			return fmt.Errorf("ui: %w", err)
		}
		a.markDirty(tree.DirtyLayout)
	}
	return nil
}

// LoadFontMany registers every family in fonts, keyed by the name a design
// writes and holding a path inside the assets directory. All valid files are
// loaded even when another path is missing; the returned error joins the
// failures.
func (a *App) LoadFontMany(fonts map[string]string) error {
	names := make([]string, 0, len(fonts))
	for name := range fonts {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []error
	for _, name := range names {
		if err := a.LoadFont(name, fonts[name]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// textLibrary is the running renderer's font library, or nil before a window is
// created or in a headless test.
func (a *App) textLibrary() *text.Library {
	d, ok := a.driver.(*gpuDriver)
	if !ok || d.gpu == nil {
		return nil
	}
	lib, ok := render.FontLibrary(d.gpu)
	if !ok {
		return nil
	}
	return lib
}
