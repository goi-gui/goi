package setup

import (
	"go/parser"
	"go/token"
	"path"
	"strconv"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// useImports registers the stdlib packages the setup body imports and that the
// interpreter does not have yet. Registering the whole stdlib.Symbols table
// costs about 1.3 MB per interpreter, and fmt alone compiles its generic
// sources again on every Use, so each package is registered once per machine.
func useImports(i *interp.Interpreter, code string, registered map[string]bool) error {
	f, err := parser.ParseFile(token.NewFileSet(), "_.go", "package setup;"+code, parser.ImportsOnly)
	if err != nil {
		// Eval reports the same syntax error with its own position.
		return nil
	}
	exports := interp.Exports{}
	if !registered["."] {
		exports["."] = stdlib.Symbols["."]
	}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		key := p + "/" + path.Base(p)
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
