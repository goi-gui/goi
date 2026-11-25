package ui

import (
	"os"
	"runtime"

	"github.com/goi-gui/goi/internal/sysfont"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// systemFonts is the installed-font lookup. It is a variable so a test can
// answer with a fixture instead of the machine's fonts.
var systemFonts = sysfont.Fonts

// loadSystemFonts registers the installed faces of every family the design
// names and the app did not register. The app's own LoadFont wins: a family
// already in the maps is left alone. Before the first window the faces are
// stored for initWindow to apply; after a reload the live library is updated
// in place.
func (a *App) loadSystemFonts(tr *tree.Tree) {
	if !a.useSystemFonts || runtime.GOOS == "js" || runtime.GOOS == "wasip1" || tr == nil {
		return
	}
	lib := a.textLibrary()
	seen := map[string]bool{}
	for _, n := range tr.All() {
		family := n.Base().FontFamily()
		// Roboto is the built-in text face and Roboto Mono the built-in
		// monospace family; a design naming either needs no lookup.
		if family == "" || family == "Roboto" || family == "Roboto Mono" || seen[family] {
			continue
		}
		seen[family] = true
		if a.hasFontFamily(family) {
			continue
		}
		files := systemFonts(family)
		if len(files) == 0 {
			continue
		}
		var faces []fontFaceFile
		var variables []fontVariable
		// A collection holds several faces of one family; read it once.
		read := map[string][]byte{}
		for _, file := range files {
			data, ok := read[file.Path]
			if !ok {
				var err error
				data, err = os.ReadFile(file.Path)
				if err != nil {
					continue
				}
				read[file.Path] = data
			}
			if file.Variable {
				variables = append(variables, fontVariable{data: data, index: file.Index, system: true})
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
				index:  file.Index,
				system: true,
			})
		}
		if len(faces) == 0 && len(variables) == 0 {
			continue
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
		if lib == nil {
			continue
		}
		// A system font that fails to parse is skipped: the design falls back
		// to Roboto instead of failing the reload.
		for _, face := range faces {
			_ = lib.RegisterFamilyFaceAt(family, face.weight, face.italic, face.data, face.index)
		}
		for _, variable := range variables {
			_ = lib.RegisterVariableFamilyAt(family, variable.data, variable.index, variable.axes)
		}
	}
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
