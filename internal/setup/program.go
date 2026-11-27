package setup

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"reflect"
	"strings"

	"github.com/goi-gui/goi/internal/gosetup"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// program is one setup file compiled once. Every fragment instance is a pair
// of closures over local variables, not another interpreter.
type program struct {
	i         *interp.Interpreter
	newFn     reflect.Value
	funcs     map[string]bool
	hasBridge bool
	data      func(string) (any, bool)
	version   func() uint64
	call      func(string, []any) (any, error)
	synced    uint64
}

type programInstance struct {
	p     *program
	value reflect.Value
	call  reflect.Value
}

func newProgram(st syntax.Setup, vars []syntax.DataVar, shapes []syntax.DataShape) (*program, error) {
	src, err := programSource(st.Code, vars, shapes)
	if err != nil {
		return nil, err
	}
	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols)
	p := &program{i: i, hasBridge: len(vars) > 0, synced: ^uint64(0)}
	i.Use(interp.Exports{
		"goisetup/goisetup": {
			"Data":     reflect.ValueOf(p.bridgeData),
			"Call":     reflect.ValueOf(p.bridgeCall),
			"AsString": reflect.ValueOf(asString),
			"AsBool":   reflect.ValueOf(asBool),
			"AsInt":    reflect.ValueOf(asInt),
			"AsFloat":  reflect.ValueOf(asFloat),
			"AsUint32": reflect.ValueOf(asUint32),
		},
	})
	if _, err := i.Eval(src); err != nil {
		return nil, evalError(st, err)
	}
	v, err := i.Eval("goiNewSetup")
	if err != nil || !v.IsValid() || v.Kind() != reflect.Func {
		return nil, fmt.Errorf("%s: setup made no instance factory", st.Src)
	}
	p.newFn = v
	p.funcs, _ = functionNames(st.Code)
	return p, nil
}

func (p *program) newInstance() *programInstance {
	if p == nil || !p.newFn.IsValid() {
		return nil
	}
	out := p.newFn.Call(nil)
	if len(out) != 1 || !out[0].IsValid() {
		return nil
	}
	state := out[0]
	if state.Kind() == reflect.Pointer {
		state = state.Elem()
	}
	v := state.FieldByName("Value")
	c := state.FieldByName("Call")
	if !v.IsValid() || !c.IsValid() {
		return nil
	}
	return &programInstance{p: p, value: v, call: c}
}

func (p *program) bind(tr *tree.Tree) {
	if p == nil || tr == nil {
		return
	}
	p.data = tr.Data
	p.version = tr.DataVersion
	p.call = tr.CallBridge
	p.synced = ^uint64(0)
	_ = p.syncBridge()
}

func (p *program) bridgeData(name string) (any, bool) {
	if p == nil || p.data == nil {
		return nil, false
	}
	return p.data(name)
}

func (p *program) bridgeCall(name string, args ...any) (any, error) {
	if p == nil || p.call == nil {
		return nil, fmt.Errorf("setup bridge is not bound to a window yet")
	}
	v, err := p.call(name, args)
	_ = p.syncBridge()
	return v, err
}

func (p *program) syncBridge() error {
	if p == nil || !p.hasBridge || p.version == nil {
		return nil
	}
	v := p.version()
	if v == p.synced {
		return nil
	}
	p.synced = v
	if _, err := p.i.Eval("(&bridge).goiSync()"); err != nil {
		return fmt.Errorf("setup bridge: %w", err)
	}
	return nil
}

func (p *programInstance) Value(name string) (any, bool) {
	if p == nil || !p.value.IsValid() {
		return nil, false
	}
	_ = p.p.syncBridge()
	out := p.value.Call([]reflect.Value{reflect.ValueOf(name)})
	if len(out) != 1 || !out[0].IsValid() || !out[0].CanInterface() {
		return nil, false
	}
	return out[0].Interface(), true
}

func (p *programInstance) HasFunc(name string) bool {
	return p != nil && p.p != nil && p.p.funcs[name]
}

func (p *programInstance) Call(name string, args []any) (any, bool, error) {
	if p == nil || !p.call.IsValid() || !p.p.funcs[name] {
		return nil, false, nil
	}
	if err := p.p.syncBridge(); err != nil {
		return nil, true, err
	}
	if args == nil {
		args = []any{}
	}
	out := p.call.Call([]reflect.Value{reflect.ValueOf(name), reflect.ValueOf(args)})
	if len(out) != 2 {
		return nil, true, fmt.Errorf("setup %s returned no result pair", name)
	}
	if !out[1].IsNil() {
		return nil, true, out[1].Interface().(error)
	}
	if !out[0].IsValid() || !out[0].CanInterface() {
		return nil, true, nil
	}
	return out[0].Interface(), true, nil
}

// programSource turns top-level setup declarations into one closure factory.
// Package variables become locals of goiNewSetup, so every call captures its
// own state while the interpreter compiles the file once.
func programSource(code string, vars []syntax.DataVar, shapes []syntax.DataShape) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "_.go", "package setup\n"+code, parser.ParseComments)
	if err != nil {
		return "", err
	}
	var imports, consts, types, varDecls []string
	var funcs []*ast.FuncDecl
	var varNodes []*ast.GenDecl
	var values []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil {
				return "", fmt.Errorf("setup method %s is not supported by the shared program", d.Name.Name)
			}
			if d.Type.Params != nil && d.Type.Params.Opening.IsValid() {
				for _, field := range d.Type.Params.List {
					if _, ok := field.Type.(*ast.Ellipsis); ok {
						return "", fmt.Errorf("setup function %s is variadic", d.Name.Name)
					}
				}
			}
			if d.Type.Results != nil && len(d.Type.Results.List) > 1 {
				return "", fmt.Errorf("setup function %s returns more than one value", d.Name.Name)
			}
			funcs = append(funcs, d)
		case *ast.GenDecl:
			text, err := nodeText(fset, d)
			if err != nil {
				return "", err
			}
			switch d.Tok {
			case token.IMPORT:
				imports = append(imports, text)
			case token.CONST:
				consts = append(consts, text)
				for _, spec := range d.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						values = append(values, name.Name)
					}
				}
			case token.TYPE:
				types = append(types, text)
				for _, spec := range d.Specs {
					values = append(values, spec.(*ast.TypeSpec).Name.Name)
				}
			case token.VAR:
				varDecls = append(varDecls, text)
				varNodes = append(varNodes, d)
				for _, spec := range d.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						values = append(values, name.Name)
					}
				}
			}
		}
	}

	isFunc := map[string]bool{}
	taken := append([]string{}, values...)
	for _, fn := range funcs {
		taken = append(taken, fn.Name.Name)
		isFunc[fn.Name.Name] = true
	}
	// A variable initializer that calls a setup function would run before the
	// function variable is assigned in the generated factory.
	for _, decl := range varNodes {
		for _, spec := range decl.Specs {
			for _, v := range spec.(*ast.ValueSpec).Values {
				early := false
				ast.Inspect(v, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok && isFunc[id.Name] {
							early = true
						}
					}
					return !early
				})
				if early {
					return "", fmt.Errorf("setup variable %s calls a setup function", spec.(*ast.ValueSpec).Names[0].Name)
				}
			}
		}
	}
	for _, name := range taken {
		// The factory below uses these names for its own locals.
		switch name {
		case "goiSetup", "goiNewSetup", "value", "call", "goiName", "goiArgs":
			return "", fmt.Errorf("setup name %s collides with the generated program", name)
		}
	}

	needsGoisetup := len(vars) > 0
	for _, fn := range funcs {
		if fn.Type.Params != nil && len(fn.Type.Params.List) > 0 {
			needsGoisetup = true
		}
	}

	var b strings.Builder
	if pre := gosetup.BridgePrelude(vars); pre != "" {
		b.WriteString(pre)
	} else if needsGoisetup {
		b.WriteString("import goisetup \"goisetup\"\n\n")
	}
	for _, imp := range imports {
		b.WriteString(imp)
		b.WriteString("\n")
	}
	for _, decl := range consts {
		b.WriteString(decl)
		b.WriteString("\n")
	}
	for _, decl := range types {
		b.WriteString(decl)
		b.WriteString("\n")
	}
	b.WriteString(gosetup.BridgeDecls(vars, shapes))
	b.WriteString("\n")
	b.WriteString("type goiSetup struct {\nValue func(string) any\nCall func(string, []any) (any, error)\n}\n\n")
	b.WriteString("func goiNewSetup() *goiSetup {\n")
	for _, decl := range varDecls {
		b.WriteString(decl)
		b.WriteString("\n")
	}
	for _, fn := range funcs {
		sig, err := nodeText(fset, fn.Type)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "var %s %s\n", fn.Name.Name, sig)
	}
	for _, fn := range funcs {
		lit := &ast.FuncLit{Type: fn.Type, Body: fn.Body}
		text, err := nodeText(fset, lit)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s = %s\n", fn.Name.Name, text)
	}

	b.WriteString("value := func(goiName string) any {\nswitch goiName {\n")
	for _, name := range values {
		fmt.Fprintf(&b, "case %q:\nreturn %s\n", name, name)
	}
	b.WriteString("}\nreturn nil\n}\n")

	b.WriteString("call := func(goiName string, goiArgs []any) (any, error) {\nswitch goiName {\n")
	for _, fn := range funcs {
		call, err := callCase(fn)
		if err != nil {
			return "", err
		}
		b.WriteString(call)
	}
	b.WriteString("}\nreturn nil, nil\n}\nreturn &goiSetup{Value: value, Call: call}\n}\n")

	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("generated setup program: %w", err)
	}
	return string(out), nil
}

func functionNames(code string) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "_.go", "package setup\n"+code, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
			out[fn.Name.Name] = true
		}
	}
	return out, nil
}

func nodeText(fset *token.FileSet, n ast.Node) (string, error) {
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, n); err != nil {
		return "", err
	}
	return b.String(), nil
}

func callCase(fn *ast.FuncDecl) (string, error) {
	var params []*ast.Field
	if fn.Type.Params != nil {
		params = fn.Type.Params.List
	}
	var args []string
	index := 0
	for _, field := range params {
		typ, ok := field.Type.(*ast.Ident)
		if !ok {
			return "", fmt.Errorf("setup function %s has a non-primitive parameter", fn.Name.Name)
		}
		conv, ok := argConversion(typ.Name, fmt.Sprintf("goiArgs[%d]", index))
		if !ok {
			return "", fmt.Errorf("setup function %s has an unsupported parameter %s", fn.Name.Name, typ.Name)
		}
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for j := 0; j < n; j++ {
			args = append(args, conv)
			index++
		}
	}
	call := fn.Name.Name + "(" + strings.Join(args, ", ") + ")"
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		return fmt.Sprintf("case %q:\nreturn %s, nil\n", fn.Name.Name, call), nil
	}
	return fmt.Sprintf("case %q:\n%s\nreturn nil, nil\n", fn.Name.Name, call), nil
}

func argConversion(name, expr string) (string, bool) {
	switch name {
	case "string":
		return "goisetup.AsString(" + expr + ")", true
	case "bool":
		return "goisetup.AsBool(" + expr + ")", true
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint64", "uintptr", "byte", "rune":
		return "goisetup.AsInt(" + expr + ")", true
	case "float32", "float64":
		return "goisetup.AsFloat(" + expr + ")", true
	case "any":
		return expr, true
	}
	return "", false
}
