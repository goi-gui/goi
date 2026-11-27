package setup

import (
	"path"

	"github.com/goi-gui/goi/internal/gosetup"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// useImports registers the stdlib packages a setup file imports and that the
// interpreter does not have yet. Registering the whole stdlib.Symbols table
// costs about 1.3 MB per interpreter, and fmt alone compiles its generic
// sources again on every Use, so each package is registered once per machine.
func useImports(i *interp.Interpreter, imports []gosetup.Import, registered map[string]bool) error {
	exports := interp.Exports{}
	if !registered["."] {
		exports["."] = stdlib.Symbols["."]
	}
	for _, im := range imports {
		key := im.Path + "/" + path.Base(im.Path)
		if registered[key] {
			continue
		}
		if syms := stdlib.Symbols[key]; syms != nil {
			exports[key] = syms
		}
	}
	if len(exports) == 0 {
		return nil
	}
	if err := i.Use(exports); err != nil {
		return err
	}
	for key := range exports {
		registered[key] = true
	}
	return nil
}
