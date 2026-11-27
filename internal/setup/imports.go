package setup

import (
	"go/parser"
	"go/token"
	"path"
	"strconv"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// useImports registers only the stdlib packages the setup body imports.
// Registering the whole stdlib.Symbols table costs about 1.3 MB of live heap
// per interpreter; one package costs kilobytes.
func useImports(i *interp.Interpreter, code string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "_.go", "package setup;"+code, parser.ImportsOnly)
	if err != nil {
		// Eval reports the same syntax error with its own position.
		return nil
	}
	exports := interp.Exports{".": stdlib.Symbols["."]}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if syms := stdlib.Symbols[p+"/"+path.Base(p)]; syms != nil {
			exports[p+"/"+path.Base(p)] = syms
		}
	}
	return i.Use(exports)
}
