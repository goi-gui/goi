package ui

import (
	"runtime"

	"github.com/goi-gui/goi/internal/sysfont"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// systemFonts is the installed-font lookup. It is a variable so a test can
// answer with a fixture instead of the machine's fonts.
var systemFonts = sysfont.Fonts

// loadSystemFonts registers the installed faces of every family the design
// names and the app did not register. A family loaded with LoadFont wins.
func (a *App) loadSystemFonts(tr *tree.Tree) {
	if !a.systemFontsAllowed() || tr == nil {
		return
	}
	seen := map[string]bool{}
	for _, n := range tr.All() {
		family := n.Base().FontFamily()
		if family == "" || seen[family] {
			continue
		}
		seen[family] = true
		a.loadSystemFamily(family)
	}
}

// loadSystemFamily resolves one family and keeps the faces for later windows.
// When a library is running the faces are registered immediately; before the
// first window initWindow applies the maps.
func (a *App) loadSystemFamily(family string) {
	// Roboto is the built-in text face and Roboto Mono the built-in monospace
	// family; a design naming either needs no lookup.
	if family == "" || family == "Roboto" || family == "Roboto Mono" || !a.systemFontsAllowed() {
		return
	}
	if a.hasFontFamily(family) {
		return
	}
	files := systemFonts(family)
	if len(files) == 0 {
		return
	}
	var faces []fontFaceFile
	var variables []fontVariable
	read := sysfont.ReadFaces(files)
	for i, file := range files {
		data := read[i].Data
		if data == nil {
			continue
		}
		if file.Variable {
			variables = append(variables, fontVariable{data: data, index: read[i].Index, system: true})
			continue
		}
		weight := text.Weight(file.Weight)
		if weight <= 0 {
			weight = text.WeightRegular
		}
		faces = append(faces, fontFaceFile{
			weight: weight,
			italic: file.Italic,
			data:   data,
			index:  read[i].Index,
			system: true,
		})
	}
	if len(faces) == 0 && len(variables) == 0 {
		return
	}
	if len(faces) > 0 {
		if a.fontFaces == nil {
			a.fontFaces = map[string][]fontFaceFile{}
		}
		a.fontFaces[family] = faces
	}
	if len(variables) > 0 {
		if a.fontVariables == nil {
			a.fontVariables = map[string][]fontVariable{}
		}
		a.fontVariables[family] = variables
	}
	lib := a.textLibrary()
	if lib == nil {
		return
	}
	// A system font that fails to parse is skipped: the design falls back to
	// Roboto instead of failing the frame.
	for _, face := range faces {
		_ = lib.RegisterFamilyFaceAt(family, face.weight, face.italic, face.data, face.index)
	}
	for _, variable := range variables {
		_ = lib.RegisterVariableFamilyAt(family, variable.data, variable.index, variable.axes)
	}
}

// systemFontsAllowed keeps the lookup out of headless tests and the browser,
// where the machine's font directory is absent or would make drawing depend on
// the test host.
func (a *App) systemFontsAllowed() bool {
	return a.useSystemFonts && runtime.GOOS != "js" && runtime.GOOS != "wasip1"
}

// resolveSystemFamily is the library's fallback for a family a Go setter names
// after the window is up.
func (a *App) resolveSystemFamily(family string) {
	a.loadSystemFamily(family)
}

// hasFontFamily reports whether the app registered family itself.
func (a *App) hasFontFamily(family string) bool {
	if _, ok := a.fonts[family]; ok {
		return true
	}
	if _, ok := a.fontFaces[family]; ok {
		return true
	}
	if _, ok := a.fontVariables[family]; ok {
		return true
	}
	return false
}
