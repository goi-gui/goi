// Package gosetup parses and type-checks a .goi setup body as a Go package.
// It is the compiler-accurate fallback for the language server: Yaegi runs the
// code, while this package answers what the editor can see before it runs.
package gosetup

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

const prefix = "package setup\n"

// Symbol is one declaration of a setup body.
type Symbol struct {
	Name   string
	Kind   string // function, variable, constant, type or package
	Detail string
}

// Item is one completion the setup body offers.
type Item struct {
	Label  string
	Kind   string // function, variable, constant, type, package or keyword
	Detail string
	Doc    string
}

// Diagnostic is a type error in a setup body, with an offset into its Code.
type Diagnostic struct {
	Offset int
	Msg    string
}

// Analysis is a parsed setup body. It is safe to keep for one completion or
// diagnostic pass; Analyzer is not safe for concurrent use.
type Analysis struct {
	fset  *token.FileSet
	file  *ast.File
	pkg   *types.Package
	info  *types.Info
	code  string
	diags []Diagnostic
}

// Analyze parses and type-checks code. A syntax error still returns the partial
// analysis when the parser produced a file, so completion keeps working while a
// line is incomplete.
func Analyze(code string) *Analysis {
	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, "_.go", prefix+code, parser.AllErrors|parser.ParseComments)
	a := &Analysis{fset: fset, file: file, code: code}
	if file == nil {
		return a
	}
	info := &types.Info{
		Defs:   map[*ast.Ident]types.Object{},
		Uses:   map[*ast.Ident]types.Object{},
		Scopes: map[ast.Node]*types.Scope{},
		Types:  map[ast.Expr]types.TypeAndValue{},
	}
	conf := types.Config{
		Importer: importer.Default(),
		Error: func(err error) {
			if e, ok := err.(types.Error); ok {
				off := fset.Position(e.Pos).Offset - len(prefix)
				if off >= 0 && off <= len(code) {
					a.diags = append(a.diags, Diagnostic{Offset: off, Msg: e.Msg})
				}
			}
		},
	}
	a.pkg, _ = conf.Check("setup", fset, []*ast.File{file}, info)
	a.info = info
	return a
}

// Diagnostics returns the type errors found while checking the body.
func (a *Analysis) Diagnostics() []Diagnostic {
	if a == nil {
		return nil
	}
	return a.diags
}

// Symbols lists the declarations visible to the rest of the design.
func (a *Analysis) Symbols() []Symbol {
	if a == nil || a.pkg == nil {
		return nil
	}
	scope := a.pkg.Scope()
	out := make([]Symbol, 0, len(scope.Names()))
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if obj == nil {
			continue
		}
		out = append(out, Symbol{Name: name, Kind: objectKind(obj), Detail: objectDetail(obj, nil)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Complete lists the names usable at offset inside the setup body.
func (a *Analysis) Complete(offset int) []Item {
	if a == nil {
		return nil
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(a.code) {
		offset = len(a.code)
	}
	start := offset
	for start > 0 && isIdent(a.code[start-1]) {
		start--
	}
	word := a.code[start:offset]
	qualifier := ""
	if start > 0 && a.code[start-1] == '.' {
		qEnd := start - 1
		qStart := qEnd
		for qStart > 0 && isIdent(a.code[qStart-1]) {
			qStart--
		}
		qualifier = a.code[qStart:qEnd]
	}

	scope := a.scopeAt(offset)
	var items []Item
	seen := map[string]bool{}
	add := func(obj types.Object, exportedOnly bool) {
		if obj == nil || seen[obj.Name()] {
			return
		}
		if exportedOnly && !obj.Exported() {
			return
		}
		if word != "" && !strings.HasPrefix(obj.Name(), word) {
			return
		}
		seen[obj.Name()] = true
		items = append(items, Item{Label: obj.Name(), Kind: objectKind(obj), Detail: objectDetail(obj, nil)})
	}

	if qualifier != "" {
		obj := a.lookup(scope, qualifier)
		if pkg, ok := obj.(*types.PkgName); ok {
			for _, name := range pkg.Imported().Scope().Names() {
				add(pkg.Imported().Scope().Lookup(name), true)
			}
		} else if obj != nil {
			a.addMembers(&items, seen, word, obj.Type())
		}
		return items
	}

	for s := scope; s != nil; s = s.Parent() {
		for _, name := range s.Names() {
			add(s.Lookup(name), false)
		}
	}
	if a.info != nil {
		for _, obj := range a.info.Implicits {
			if pn, ok := obj.(*types.PkgName); ok {
				add(pn, false)
			}
		}
	}
	items = append(items, a.keywordItems(word)...)
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return items
}

func (a *Analysis) scopeAt(offset int) *types.Scope {
	if a == nil || a.pkg == nil {
		return nil
	}
	pos := token.Pos(len(prefix) + offset)
	best := a.pkg.Scope()
	bestWidth := best.End() - best.Pos()
	for _, scope := range a.info.Scopes {
		if scope.Pos() > pos || pos > scope.End() {
			continue
		}
		if width := scope.End() - scope.Pos(); width < bestWidth {
			best, bestWidth = scope, width
		}
	}
	return best
}

func lookupInChain(scope *types.Scope, name string) types.Object {
	for s := scope; s != nil; s = s.Parent() {
		if obj := s.Lookup(name); obj != nil {
			return obj
		}
	}
	return nil
}

func (a *Analysis) lookup(scope *types.Scope, name string) types.Object {
	if obj := lookupInChain(scope, name); obj != nil {
		return obj
	}
	if a.info != nil {
		for _, obj := range a.info.Implicits {
			if obj != nil && obj.Name() == name {
				return obj
			}
		}
		for ident, obj := range a.info.Uses {
			if ident.Name == name && obj != nil {
				return obj
			}
		}
		for ident, obj := range a.info.Defs {
			if ident.Name == name && obj != nil {
				return obj
			}
		}
	}
	return nil
}

func (a *Analysis) addMembers(items *[]Item, seen map[string]bool, word string, typ types.Type) {
	if typ == nil {
		return
	}
	base := typ
	if p, ok := typ.(*types.Pointer); ok {
		base = p.Elem()
	}
	named, ok := base.(*types.Named)
	if !ok {
		return
	}
	if st, ok := named.Underlying().(*types.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			if !f.Exported() || (word != "" && !strings.HasPrefix(f.Name(), word)) {
				continue
			}
			if seen[f.Name()] {
				continue
			}
			seen[f.Name()] = true
			*items = append(*items, Item{Label: f.Name(), Kind: "variable", Detail: objectDetail(f, nil)})
		}
	}
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		if !m.Exported() || (word != "" && !strings.HasPrefix(m.Name(), word)) {
			continue
		}
		if seen[m.Name()] {
			continue
		}
		seen[m.Name()] = true
		*items = append(*items, Item{Label: m.Name(), Kind: "function", Detail: objectDetail(m, nil)})
	}
}

func (a *Analysis) keywordItems(word string) []Item {
	keywords := []string{
		"break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for", "func", "go", "goto", "if", "import", "interface", "map", "package", "range", "return", "select", "struct", "switch", "type", "var",
	}
	types := []string{
		"any", "bool", "byte", "complex64", "complex128", "error", "float32", "float64", "int", "int8", "int16", "int32", "int64", "rune", "string", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	}
	builtins := []string{"append", "cap", "close", "complex", "copy", "delete", "imag", "len", "make", "new", "panic", "print", "println", "real", "recover", "true", "false", "nil", "iota"}
	out := make([]Item, 0, len(keywords)+len(types)+len(builtins))
	add := func(kind string, names []string) {
		for _, name := range names {
			if word == "" || strings.HasPrefix(name, word) {
				out = append(out, Item{Label: name, Kind: kind})
			}
		}
	}
	add("keyword", keywords)
	add("type", types)
	add("keyword", builtins)
	return out
}

func objectKind(obj types.Object) string {
	if obj == nil {
		return "variable"
	}
	switch obj.(type) {
	case *types.Func:
		return "function"
	case *types.TypeName:
		return "type"
	case *types.Const:
		return "constant"
	case *types.PkgName:
		return "package"
	default:
		return "variable"
	}
}

func objectDetail(obj types.Object, _ types.Qualifier) string {
	if obj == nil {
		return ""
	}
	if obj.Type() == nil {
		return ""
	}
	return types.TypeString(obj.Type(), nil)
}

func isIdent(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
