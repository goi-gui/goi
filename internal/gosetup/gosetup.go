// Package gosetup parses and type-checks a .goi setup body as a Go package.
// It is the compiler-accurate fallback for the language server: Yaegi runs the
// code, while this package answers what the editor can see before it runs.
package gosetup

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
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
	base  int
	diags []Diagnostic
}

// Analyze parses and type-checks code. A syntax error still returns the partial
// analysis when the parser produced a file, so completion keeps working while a
// line is incomplete.
func Analyze(code string) *Analysis {
	body, imports := PrepareImports(code)
	block := ImportBlock(imports)
	return analyze(code, prefix+block+body, len(prefix)+len(block))
}

// AnalyzeWithBridge is Analyze with the design's bridge declarations exposed as
// the `bridge` value, so setup code that reads a variable or calls a handler
// type-checks.
func AnalyzeWithBridge(code string, vars []syntax.DataVar, shapes []syntax.DataShape, props []syntax.Prop) *Analysis {
	body, imports := PrepareImports(code)
	pre, decl := bridgeParts(vars, shapes, props, "goisetup")
	block := ImportBlock(imports)
	return analyze(code, prefix+block+pre+body+"\n"+decl, len(prefix)+len(block)+len(pre))
}

func analyze(code, source string, base int) *Analysis {
	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, "_.go", source, parser.AllErrors|parser.ParseComments)
	a := &Analysis{fset: fset, file: file, code: code, base: base}
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
		Importer: bridgeImporter{base: importer.Default()},
		Error: func(err error) {
			if e, ok := err.(types.Error); ok {
				off := fset.Position(e.Pos).Offset - base
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
	pos := token.Pos(a.base + offset)
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
			if (!f.Exported() && f.Pkg() != a.pkg) || (word != "" && !strings.HasPrefix(f.Name(), word)) {
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
		if (!m.Exported() && m.Pkg() != a.pkg) || (word != "" && !strings.HasPrefix(m.Name(), word)) {
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

// BridgeSource is the Go source a setup scope evaluates: the user body plus a
// generated `bridge` value and methods for the design's declarations.
func BridgeSource(code string, vars []syntax.DataVar, shapes []syntax.DataShape, props []syntax.Prop) string {
	return BridgeSourcePkg(code, vars, shapes, props, "goisetup")
}

// BridgeSourcePkg is BridgeSource with a named synthetic package, so several
// setup files in one interpreter can each bind their own Data and Call.
func BridgeSourcePkg(code string, vars []syntax.DataVar, shapes []syntax.DataShape, props []syntax.Prop, pkg string) string {
	pre, decl := bridgeParts(vars, shapes, props, pkg)
	return pre + code + "\n" + decl
}

// BridgePrelude is the generated import a setup program needs for bridge access.
func BridgePrelude(vars []syntax.DataVar, props []syntax.Prop) string {
	pre, _ := bridgeParts(vars, nil, props, "goisetup")
	return pre
}

// BridgeDecls is the generated bridge value and its methods.
func BridgeDecls(vars []syntax.DataVar, shapes []syntax.DataShape, props []syntax.Prop) string {
	_, decl := bridgeParts(vars, shapes, props, "goisetup")
	return decl
}

// BridgeParts is the import and declarations a setup program needs, generated
// once because the shape converters are not cheap to write twice.
func BridgeParts(vars []syntax.DataVar, shapes []syntax.DataShape, props []syntax.Prop, pkg string) (pre, decl string) {
	return bridgeParts(vars, shapes, props, pkg)
}

func bridgeParts(vars []syntax.DataVar, shapes []syntax.DataShape, props []syntax.Prop, pkg string) (pre, decl string) {
	if len(vars) == 0 && len(shapes) == 0 && len(props) == 0 {
		return "", ""
	}
	shapeMap := make(map[string]*syntax.DataShape, len(shapes))
	for i := range shapes {
		shapeMap[shapes[i].Name] = &shapes[i]
	}
	var types strings.Builder
	for _, s := range shapes {
		fmt.Fprintf(&types, "type %s struct {\n", s.Name)
		for _, f := range s.Fields {
			fmt.Fprintf(&types, "\t%s %s\n", f.Name, goType(f.Type, shapeMap))
		}
		types.WriteString("}\n\n")
	}
	conv := &convGen{shapes: shapeMap, pkg: pkg, names: map[string]string{}}
	var propDecl strings.Builder
	if len(props) > 0 {
		propDecl.WriteString("type goiProps struct {\n")
		for _, pr := range props {
			fmt.Fprintf(&propDecl, "\t%s %s\n", pr.Name, goType(pr.Type, shapeMap))
		}
		propDecl.WriteString("}\n\nvar props goiProps\n\n")
		propDecl.WriteString("func goiSetProps(m map[string]any) {\n")
		for _, pr := range props {
			fmt.Fprintf(&propDecl, "props.%s = %s\n", pr.Name,
				conv.expr(pr.Type, fmt.Sprintf("%s.Field(m, %q)", pkg, pr.Name)))
		}
		propDecl.WriteString("}\n\n")
	}
	var fields, sync, methods strings.Builder
	for _, v := range vars {
		if v.Type.Kind == syntax.TypeFunc {
			params, args := methodParams(v.Type.Args, shapeMap)
			result := ""
			if v.Type.Result != nil {
				result = " " + goType(*v.Type.Result, shapeMap)
			}
			fmt.Fprintf(&methods, "func (b goiBridge) %s(%s)%s {\n", v.Name, params, result)
			call := pkg + ".Call(" + strconv.Quote(v.Name) + args + ")"
			if v.Type.Result == nil {
				fmt.Fprintf(&methods, "\tif _, err := %s; err != nil { panic(err) }\n", call)
			} else {
				fmt.Fprintf(&methods, "\tv, err := %s\n\tif err != nil { panic(err) }\n\treturn %s\n",
					call, conv.expr(*v.Type.Result, "v"))
			}
			methods.WriteString("}\n")
			continue
		}
		fmt.Fprintf(&fields, "\t%s %s\n", v.Name, goType(v.Type, shapeMap))
		fmt.Fprintf(&sync, "\tif v, ok := "+pkg+".Data(%q); ok { b.%s = %s }\n",
			v.Name, v.Name, conv.expr(v.Type, "v"))
	}
	if len(vars) > 0 || len(props) > 0 {
		pre = "import " + pkg + " \"" + pkg + "\"\n\n"
	}
	decl = types.String() + conv.funcsString() + propDecl.String() +
		"type goiBridge struct {\n" + fields.String() + "}\n\n" +
		"var bridge goiBridge\n\n" +
		"func (b *goiBridge) goiSync() {\n" + sync.String() + "}\n\n" +
		methods.String()
	return pre, decl
}

// convGen writes Go functions that turn the untyped rows the bridge callback
// returns into the type the design declared: []ticket, []string, map[string]int.
type convGen struct {
	shapes map[string]*syntax.DataShape
	pkg    string
	names  map[string]string
	funcs  []string
	next   int
}

func (c *convGen) funcsString() string { return strings.Join(c.funcs, "\n") }

func (c *convGen) conv(t syntax.Type) string {
	key := t.String()
	if name, ok := c.names[key]; ok {
		return name
	}
	name := fmt.Sprintf("goiConv%d", c.next)
	c.next++
	c.names[key] = name
	gt := goType(t, c.shapes)
	var b strings.Builder
	switch t.Kind {
	case syntax.TypeSlice:
		fmt.Fprintf(&b, "func %s(v any) %s {\n", name, gt)
		fmt.Fprintf(&b, "if x, ok := v.(%s); ok { return x }\n", gt)
		b.WriteString("rows, _ := v.([]any)\n")
		fmt.Fprintf(&b, "out := make(%s, 0, len(rows))\n", gt)
		b.WriteString("for _, r := range rows {\n")
		fmt.Fprintf(&b, "out = append(out, %s)\n", c.expr(*t.Elem, "r"))
		b.WriteString("}\nreturn out\n}\n")
	case syntax.TypeMap:
		fmt.Fprintf(&b, "func %s(v any) %s {\n", name, gt)
		fmt.Fprintf(&b, "if x, ok := v.(%s); ok { return x }\n", gt)
		if t.Name == "string" {
			b.WriteString("m, _ := v.(map[string]any)\n")
			fmt.Fprintf(&b, "out := make(%s, len(m))\n", gt)
			fmt.Fprintf(&b, "for k, r := range m { out[k] = %s }\n", c.expr(*t.Elem, "r"))
			b.WriteString("return out\n}\n")
		} else {
			fmt.Fprintf(&b, "return make(%s)\n}\n", gt)
		}
	case syntax.TypeScalar:
		shape := c.shapes[t.Name]
		fmt.Fprintf(&b, "func %s(v any) %s {\n", name, gt)
		if shape == nil {
			fmt.Fprintf(&b, "x, _ := v.(%s)\nreturn x\n}\n", gt)
			break
		}
		fmt.Fprintf(&b, "if x, ok := v.(%s); ok { return x }\n", gt)
		b.WriteString("r, _ := v.(map[string]any)\n")
		fmt.Fprintf(&b, "return %s{\n", shape.Name)
		for _, f := range shape.Fields {
			fmt.Fprintf(&b, "%s: %s,\n", f.Name, c.expr(f.Type, fmt.Sprintf("%s.Field(r, %q)", c.pkg, f.Name)))
		}
		b.WriteString("}\n}\n")
	}
	c.funcs = append(c.funcs, b.String())
	return name
}

func (c *convGen) expr(t syntax.Type, v string) string {
	if t.Kind == syntax.TypeScalar && c.shapes[t.Name] == nil {
		return scalarExpr(t, v, c.pkg)
	}
	if t.Kind == syntax.TypeFunc {
		return v
	}
	return c.conv(t) + "(" + v + ")"
}

func methodParams(fields []syntax.Field, shapes map[string]*syntax.DataShape) (params, args string) {
	var ps, as []string
	for _, f := range fields {
		ps = append(ps, f.Name+" "+goType(f.Type, shapes))
		as = append(as, ", "+f.Name)
	}
	return strings.Join(ps, ", "), strings.Join(as, "")
}

func goType(t syntax.Type, shapes map[string]*syntax.DataShape) string {
	switch t.Kind {
	case syntax.TypeSlice:
		return "[]" + goType(*t.Elem, shapes)
	case syntax.TypeMap:
		return "map[" + t.Name + "]" + goType(*t.Elem, shapes)
	case syntax.TypeFunc:
		return "any"
	}
	switch t.Name {
	case "string", "bool", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64", "byte", "rune", "any":
		return t.Name
	case "color":
		return "uint32"
	}
	if _, ok := shapes[t.Name]; ok {
		return t.Name
	}
	return "any"
}

// scalarExpr converts one untyped value to the exact scalar the design
// declared, keeping int32, uint8 and friends assignable.
func scalarExpr(t syntax.Type, v, pkg string) string {
	switch t.Name {
	case "string":
		return pkg + ".AsString(" + v + ")"
	case "bool":
		return pkg + ".AsBool(" + v + ")"
	case "color", "uint32":
		return pkg + ".AsUint32(" + v + ")"
	case "int":
		return pkg + ".AsInt(" + v + ")"
	case "int8":
		return "int8(" + pkg + ".AsInt(" + v + "))"
	case "int16":
		return "int16(" + pkg + ".AsInt(" + v + "))"
	case "int32":
		return "int32(" + pkg + ".AsInt(" + v + "))"
	case "int64":
		return "int64(" + pkg + ".AsInt(" + v + "))"
	case "uint":
		return "uint(" + pkg + ".AsInt(" + v + "))"
	case "uint8", "byte":
		return "byte(" + pkg + ".AsInt(" + v + "))"
	case "uint16":
		return "uint16(" + pkg + ".AsInt(" + v + "))"
	case "uint64":
		return "uint64(" + pkg + ".AsInt(" + v + "))"
	case "uintptr":
		return "uintptr(" + pkg + ".AsInt(" + v + "))"
	case "rune":
		return "rune(" + pkg + ".AsInt(" + v + "))"
	case "float32":
		return "float32(" + pkg + ".AsFloat(" + v + "))"
	case "float64":
		return pkg + ".AsFloat(" + v + ")"
	}
	return v
}

type bridgeImporter struct{ base types.Importer }

func (i bridgeImporter) Import(path string) (*types.Package, error) {
	if path == "goisetup" {
		return bridgePackage(), nil
	}
	return i.base.Import(path)
}

var fakeBridge *types.Package

func bridgePackage() *types.Package {
	if fakeBridge != nil {
		return fakeBridge
	}
	pkg := types.NewPackage("goisetup", "goisetup")
	anyT := types.NewInterfaceType(nil, nil)
	anyT.Complete()
	errT := types.Universe.Lookup("error").Type()
	sig := func(params []*types.Var, results []*types.Var, variadic bool) *types.Signature {
		return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), types.NewTuple(results...), variadic)
	}
	obj := func(name string, params []*types.Var, results []*types.Var, variadic bool) {
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig(params, results, variadic)))
	}
	anyParam := func(name string) *types.Var { return types.NewVar(0, pkg, name, anyT) }
	nameParam := func() *types.Var { return types.NewVar(0, pkg, "name", types.Typ[types.String]) }
	obj("Data", []*types.Var{nameParam()}, []*types.Var{anyParam(""), types.NewVar(0, pkg, "", types.Typ[types.Bool])}, false)
	obj("Call", []*types.Var{nameParam(), types.NewVar(0, pkg, "args", types.NewSlice(anyT))},
		[]*types.Var{anyParam(""), types.NewVar(0, pkg, "", errT)}, true)
	obj("AsString", []*types.Var{anyParam("")}, []*types.Var{types.NewVar(0, pkg, "", types.Typ[types.String])}, false)
	obj("AsBool", []*types.Var{anyParam("")}, []*types.Var{types.NewVar(0, pkg, "", types.Typ[types.Bool])}, false)
	obj("AsInt", []*types.Var{anyParam("")}, []*types.Var{types.NewVar(0, pkg, "", types.Typ[types.Int])}, false)
	obj("AsFloat", []*types.Var{anyParam("")}, []*types.Var{types.NewVar(0, pkg, "", types.Typ[types.Float64])}, false)
	obj("AsUint32", []*types.Var{anyParam("")}, []*types.Var{types.NewVar(0, pkg, "", types.Typ[types.Uint32])}, false)
	pkg.MarkComplete()
	fakeBridge = pkg
	return pkg
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
