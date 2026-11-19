package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

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
	assets, err := a.fontAssets()
	if err != nil {
		return err
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

// fontFaceFile is one weight/style file a family loaded.
type fontFaceFile struct {
	weight text.Weight
	italic bool
	data   []byte
}

// fontVariable is a variable font plus the axes pinned for every weight. wght
// comes from fontWeight; the other axes are fixed.
type fontVariable struct {
	data []byte
	axes map[string]float64
}

// fontAssets is the container LoadFont resolves paths in: the live window's
// assets, the embedded build assets, or the assets directory from goi.yaml.
func (a *App) fontAssets() (fs.FS, error) {
	if a.assets != nil {
		return a.assets, nil
	}
	if fsys := registeredAssets(); fsys != nil {
		return fsys, nil
	}
	dir, err := designDir()
	if err != nil {
		return nil, err
	}
	return dirFS(dir), nil
}

// LoadFontFamily registers every file matching pattern under one family. The
// pattern is a glob inside the assets directory, usually:
//
//	app.LoadFontFamily("Nunito", "fonts/Nunito-*.ttf")
//
// The style suffix after the last hyphen picks a weight and italic:
//
//	Nunito-Regular.ttf     400
//	Nunito-Medium.ttf      500
//	Nunito-SemiBold.ttf    600
//	Nunito-Bold.ttf        700
//	Nunito-BoldItalic.ttf  700 italic
//
// The design still writes only fontFamily: "Nunito"; fontWeight and fontStyle
// select the file. A suffix the parser does not know is skipped.
func (a *App) LoadFontFamily(name, pattern string) error {
	if name == "" {
		return fmt.Errorf("ui: LoadFontFamily needs a family name")
	}
	pattern = cleanName(filepath.ToSlash(pattern))
	if pattern == "" || pattern == "." {
		return fmt.Errorf("ui: LoadFontFamily(%q) needs a glob pattern", name)
	}
	assets, err := a.fontAssets()
	if err != nil {
		return err
	}
	paths, err := fs.Glob(assets, pattern)
	if err != nil {
		return fmt.Errorf("ui: LoadFontFamily(%q): %w", name, err)
	}
	var faces []fontFaceFile
	for _, file := range paths {
		ext := path.Ext(file)
		stem := strings.TrimSuffix(path.Base(file), ext)
		i := strings.LastIndexByte(stem, '-')
		if i < 0 || i == len(stem)-1 {
			continue
		}
		weight, italic, ok := fontStyle(stem[i+1:])
		if !ok {
			continue
		}
		data, err := fs.ReadFile(assets, file)
		if err != nil {
			return fmt.Errorf("ui: LoadFontFamily(%q): %w", name, err)
		}
		faces = append(faces, fontFaceFile{weight: weight, italic: italic, data: data})
	}
	if len(faces) == 0 {
		return fmt.Errorf("ui: LoadFontFamily(%q): no weight or style files match %s", name, pattern)
	}
	if a.fontFaces == nil {
		a.fontFaces = map[string][]fontFaceFile{}
	}
	a.fontFaces[name] = faces
	if lib := a.textLibrary(); lib != nil {
		for _, face := range faces {
			if err := lib.RegisterFamilyFace(name, face.weight, face.italic, face.data); err != nil {
				return fmt.Errorf("ui: %w", err)
			}
		}
		a.markDirty(tree.DirtyLayout)
	}
	return nil
}

// LoadVariableFont registers a variable font under name. fontWeight drives its
// wght axis; axes pins any other axes, such as wdth or opsz. A file whose own
// style is italic fills the family's italic slot, so calling this again with
// the same name and the italic file gives real italics. Otherwise
// fontStyle: italic slants the upright face.
//
//	app.LoadVariableFont("Nunito", "fonts/Nunito[wght].ttf", nil)
//	app.LoadVariableFont("Nunito", "fonts/Nunito-Italic[wght].ttf", nil)
//	app.LoadVariableFont("Text", "fonts/Text[wdth,wght].ttf", map[string]float64{"wdth": 90})
func (a *App) LoadVariableFont(name, fontPath string, axes map[string]float64) error {
	if name == "" {
		return fmt.Errorf("ui: LoadVariableFont needs a family name")
	}
	if fontPath == "" {
		return fmt.Errorf("ui: LoadVariableFont(%q) needs a path", name)
	}
	for axis := range axes {
		if len(axis) != 4 {
			return fmt.Errorf("ui: LoadVariableFont(%q): axis %q must be four characters", name, axis)
		}
	}
	assets, err := a.fontAssets()
	if err != nil {
		return err
	}
	data, err := fs.ReadFile(assets, cleanName(filepath.ToSlash(fontPath)))
	if err != nil {
		return fmt.Errorf("ui: LoadVariableFont(%q): %w", name, err)
	}
	fixed := make(map[string]float64, len(axes))
	for axis, value := range axes {
		fixed[axis] = value
	}
	lib := a.textLibrary()
	if lib != nil {
		if err := lib.RegisterVariableFamily(name, data, fixed); err != nil {
			return fmt.Errorf("ui: %w", err)
		}
	} else if err := text.ValidateVariableFont(data, fixed); err != nil {
		// A window may not exist yet; still reject a bad file here so the
		// caller gets the error from LoadVariableFont.
		return fmt.Errorf("ui: LoadVariableFont(%q): %w", name, err)
	}
	if a.fontVariables == nil {
		a.fontVariables = map[string][]fontVariable{}
	}
	a.fontVariables[name] = append(a.fontVariables[name], fontVariable{data: data, axes: fixed})
	if lib != nil {
		a.markDirty(tree.DirtyLayout)
	}
	return nil
}

// LoadFontFamilyMany registers several families, keyed by the design's family
// name and holding the glob pattern for its files. All valid families are
// loaded even when another pattern fails; the returned error joins failures.
func (a *App) LoadFontFamilyMany(families map[string]string) error {
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []error
	for _, name := range names {
		if err := a.LoadFontFamily(name, families[name]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// fontStyle turns a Google Fonts style suffix into a weight and italic flag.
func fontStyle(s string) (text.Weight, bool, bool) {
	s = strings.TrimSpace(s)
	italic := false
	if strings.HasSuffix(strings.ToLower(s), "italic") {
		italic = true
		s = s[:len(s)-len("italic")]
	}
	s = strings.ToLower(strings.Trim(s, "-_ "))
	switch s {
	case "", "regular", "normal":
		return text.WeightRegular, italic, true
	case "thin", "hairline":
		return text.Weight(100), italic, true
	case "extralight", "ultralight":
		return text.Weight(200), italic, true
	case "light":
		return text.WeightLight, italic, true
	case "medium":
		return text.WeightMedium, italic, true
	case "semibold", "demibold":
		return text.Weight(600), italic, true
	case "bold":
		return text.WeightBold, italic, true
	case "extrabold", "ultrabold":
		return text.Weight(800), italic, true
	case "black", "heavy":
		return text.WeightBlack, italic, true
	}
	return 0, false, false
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
