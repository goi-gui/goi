package ui

import (
	"fmt"

	"github.com/goi-gui/goi/internal/render"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// LoadFont registers an app font under name, which is the family a design writes
// as `fontFamily`. A .goi file cannot carry a font, so the Go side hands the
// bytes over; the design only names them.
//
//	inter, err := os.ReadFile("Inter.ttf")
//	if err != nil {
//		return err
//	}
//	if err := app.LoadFont("Inter", inter); err != nil {
//		return err
//	}
//
//	// Window { fontFamily: "Inter" }            // every text in the window
//	// Text { text: "Hi" fontFamily: "Inter" }  // one element only
//
// One file serves every weight: a single face cannot vary, so a bold run draws
// the same outlines as a regular one. A family registered after the window is
// up takes effect on the next frame.
func (a *App) LoadFont(name string, data []byte) error {
	if name == "" {
		return fmt.Errorf("ui: LoadFont needs a family name")
	}
	if len(data) == 0 {
		return fmt.Errorf("ui: LoadFont(%q): no font data", name)
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
