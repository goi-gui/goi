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
	"github.com/traefik/yaegi/interp"
)

// program is one setup file compiled once into the shared machine. Every
// fragment instance is a pair of closures over local variables, not another
// interpreter.
type program struct {
	m         *machine
	st        syntax.Setup
	code      string
	src       string
	pkg       string
	bridgePkg string
	imports   []gosetup.Import
	funcs     map[string]bool
	hasBridge bool

	newFn      reflect.Value
	compiled   bool
	compileErr error
	synced     uint64
}

type programInstance struct {
	p       *program
	value   reflect.Value
	call    reflect.Value
	mount   reflect.Value
	unmount reflect.Value
}

func newProgram(st syntax.Setup, vars []syntax.DataVar, shapes []syntax.DataShape, index int, m *machine) (*program, error) {
	bridgePkg := fmt.Sprintf("goisetup%d", index)
	body, imports := gosetup.PrepareImports(st.Code)
	src, err := programSource(st, body, imports, vars, shapes, bridgePkg)
	if err != nil {
		return nil, err
	}
	p := &program{
		m:         m,
		st:        st,
		code:      body,
		src:       src,
		imports:   imports,
		pkg:       fmt.Sprintf("goi%d", index),
		bridgePkg: bridgePkg,
		hasBridge: len(vars) > 0,
		synced:    ^uint64(0),
	}
	p.funcs, _ = functionNames(st.Code)
	return p, nil
}

// compile evaluates this file in the shared machine on first use. A design
// with a hundred fragments only pays for the ones the page actually shows.
func (p *program) compile() error {
	if p == nil {
		return nil
	}
	if p.compiled {
		return p.compileErr
	}
	p.compiled = true
	m := p.m
	if err := useImports(m.i, p.imports, m.registered); err != nil {
		p.compileErr = evalError(p.st, err)
		return p.compileErr
	}
	if err := m.i.Use(interp.Exports{
		p.bridgePkg + "/" + p.bridgePkg: {
			"Data":     reflect.ValueOf(p.bridgeData),
			"Call":     reflect.ValueOf(p.bridgeCall),
			"AsString": reflect.ValueOf(asString),
			"AsBool":   reflect.ValueOf(asBool),
			"AsInt":    reflect.ValueOf(asInt),
			"AsFloat":  reflect.ValueOf(asFloat),
			"AsUint32": reflect.ValueOf(asUint32),
		},
	}); err != nil {
		p.compileErr = err
		return err
	}
	if _, err := m.i.Eval("package " + p.pkg + "\n" + p.src); err != nil {
		p.compileErr = evalError(p.st, err)
		return p.compileErr
	}
	v, err := m.i.Eval(p.pkg + ".goiNewSetup")
	if err != nil || !v.IsValid() || v.Kind() != reflect.Func {
		p.compileErr = fmt.Errorf("%s: setup made no instance factory", p.st.Src)
		return p.compileErr
	}
	p.newFn = v
	return nil
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
	return &programInstance{
		p:       p,
		value:   v,
		call:    c,
		mount:   state.FieldByName("Mount"),
		unmount: state.FieldByName("Unmount"),
	}
}

func (p *program) bridgeData(name string) (any, bool) {
	if p == nil || p.m.data == nil {
		return nil, false
	}
	v, ok := p.m.data(name)
	if !ok {
		return nil, false
	}
	return bridgeDataValue(v), true
}

func (p *program) bridgeCall(name string, args ...any) (any, error) {
	if p == nil || p.m.call == nil {
		return nil, fmt.Errorf("setup bridge is not bound to a window yet")
	}
	v, err := p.m.call(name, args)
	_ = p.syncBridge()
	return bridgeDataValue(v), err
}

func (p *program) syncBridge() error {
	if p == nil || !p.hasBridge || p.m.version == nil {
		return nil
	}
	v := p.m.version()
	if v == p.synced {
		return nil
	}
	p.synced = v
	if _, err := p.m.i.Eval(p.pkg + ".goiSyncBridge()"); err != nil {
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
func programSource(st syntax.Setup, body string, imports []gosetup.Import, vars []syntax.DataVar, shapes []syntax.DataShape, bridgePkg string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "_.go", "package setup\n"+body, parser.ParseComments)
	if err != nil {
		return "", err
	}
	var consts, types, varDecls []programDecl
	var funcs, methods []*ast.FuncDecl
	hooks := map[string]*ast.FuncDecl{}
	var varNodes []*ast.GenDecl
	var values []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil {
				methods = append(methods, d)
				continue
			}
			if isLifecycle(d.Name.Name) {
				if (d.Type.Params != nil && len(d.Type.Params.List) > 0) ||
					(d.Type.Results != nil && len(d.Type.Results.List) > 0) {
					return "", fmt.Errorf("setup %s must take no parameters and return nothing", d.Name.Name)
				}
				hooks[d.Name.Name] = d
				continue
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
			block := programDecl{text: text, line: fset.Position(d.Pos()).Line}
			switch d.Tok {
			case token.CONST:
				consts = append(consts, block)
				for _, spec := range d.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						values = append(values, name.Name)
					}
				}
			case token.TYPE:
				types = append(types, block)
				for _, spec := range d.Specs {
					values = append(values, spec.(*ast.TypeSpec).Name.Name)
				}
			case token.VAR:
				varDecls = append(varDecls, block)
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
	for _, m := range methods {
		taken = append(taken, m.Name.Name)
	}
	for name := range hooks {
		taken = append(taken, name)
	}
	// A method lives at package level, where the factory's variables and
	// functions are not visible; one that reads them keeps the interpreter.
	varNames := map[string]bool{}
	for _, d := range varNodes {
		for _, spec := range d.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				varNames[name.Name] = true
			}
		}
	}
	for _, m := range methods {
		blocked := false
		ast.Inspect(m.Body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && (varNames[id.Name] || isFunc[id.Name]) {
				blocked = true
			}
			return !blocked
		})
		if blocked {
			return "", fmt.Errorf("setup method %s reads setup state", m.Name.Name)
		}
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
		case "goiSetup", "goiNewSetup", "value", "call", "goiName", "goiArgs", "goiRest", "goiArg",
			"goiMount", "goiUnmount":
			return "", fmt.Errorf("setup name %s collides with the generated program", name)
		}
	}

	needsGoisetup := len(vars) > 0
	for _, fn := range funcs {
		if fn.Type.Params != nil && len(fn.Type.Params.List) > 0 {
			needsGoisetup = true
		}
	}

	pre, decl := gosetup.BridgeParts(vars, shapes, bridgePkg)
	var b strings.Builder
	b.WriteString(gosetup.ImportBlock(imports))
	if pre != "" {
		b.WriteString(pre)
	} else if needsGoisetup {
		b.WriteString("import " + bridgePkg + " \"" + bridgePkg + "\"\n\n")
	}
	for _, decl := range consts {
		writeLine(&b, st, decl.line)
		b.WriteString(decl.text)
		b.WriteString("\n")
	}
	for _, decl := range types {
		writeLine(&b, st, decl.line)
		b.WriteString(decl.text)
		b.WriteString("\n")
	}
	for _, m := range methods {
		writeLine(&b, st, fset.Position(m.Pos()).Line)
		text, err := nodeText(fset, m)
		if err != nil {
			return "", err
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	b.WriteString(decl)
	b.WriteString("\n")
	if len(vars) > 0 {
		b.WriteString("func goiSyncBridge() { (&bridge).goiSync() }\n\n")
	}
	b.WriteString("type goiSetup struct {\nValue func(string) any\nCall func(string, []any) (any, error)\nMount func()\nUnmount func()\n}\n\n")
	b.WriteString("func goiNewSetup() *goiSetup {\n")
	for _, decl := range varDecls {
		writeLine(&b, st, decl.line)
		b.WriteString(decl.text)
		b.WriteString("\n")
	}
	for _, fn := range funcs {
		sig, err := nodeText(fset, fn.Type)
		if err != nil {
			return "", err
		}
		writeLine(&b, st, fset.Position(fn.Pos()).Line)
		fmt.Fprintf(&b, "var %s %s\n", fn.Name.Name, sig)
	}
	for _, fn := range funcs {
		lit := &ast.FuncLit{Type: fn.Type, Body: fn.Body}
		text, err := nodeText(fset, lit)
		if err != nil {
			return "", err
		}
		writeLine(&b, st, fset.Position(fn.Pos()).Line)
		fmt.Fprintf(&b, "%s = %s\n", fn.Name.Name, text)
	}
	b.WriteString("var goiMount func()\nvar goiUnmount func()\n")
	for _, name := range []string{"onMount", "onUnmount"} {
		fn := hooks[name]
		if fn == nil {
			continue
		}
		lit := &ast.FuncLit{Type: fn.Type, Body: fn.Body}
		text, err := nodeText(fset, lit)
		if err != nil {
			return "", err
		}
		writeLine(&b, st, fset.Position(fn.Pos()).Line)
		local := "goiMount"
		if name == "onUnmount" {
			local = "goiUnmount"
		}
		fmt.Fprintf(&b, "%s = %s\n", local, text)
	}

	b.WriteString("value := func(goiName string) any {\nswitch goiName {\n")
	for _, name := range values {
		fmt.Fprintf(&b, "case %q:\nreturn %s\n", name, name)
	}
	b.WriteString("}\nreturn nil\n}\n")

	b.WriteString("call := func(goiName string, goiArgs []any) (any, error) {\nswitch goiName {\n")
	for _, fn := range funcs {
		call, err := callCase(fn, bridgePkg)
		if err != nil {
			return "", err
		}
		b.WriteString(call)
	}
	b.WriteString("}\nreturn nil, nil\n}\n")
	b.WriteString("return &goiSetup{Value: value, Call: call, Mount: goiMount, Unmount: goiUnmount}\n}\n")

	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("generated setup program: %w", err)
	}
	return string(out), nil
}

type programDecl struct {
	text string
	line int
}

// writeLine pins the next generated line to its line in the design file, so a
// Yaegi error names the setup block instead of the generated source.
func writeLine(b *strings.Builder, st syntax.Setup, parseLine int) {
	line := st.Line + parseLine - 2
	if line < 1 {
		line = 1
	}
	fmt.Fprintf(b, "//line %s:%d\n", st.Src, line)
}

func functionNames(code string) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "_.go", "package setup\n"+code, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && !isLifecycle(fn.Name.Name) {
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

func callCase(fn *ast.FuncDecl, bridgePkg string) (string, error) {
	var params []*ast.Field
	if fn.Type.Params != nil {
		params = fn.Type.Params.List
	}
	var setup strings.Builder
	var args []string
	index := 0
	for i, field := range params {
		if ell, ok := field.Type.(*ast.Ellipsis); ok {
			if i != len(params)-1 {
				return "", fmt.Errorf("setup function %s has a variadic parameter before the end", fn.Name.Name)
			}
			typ, ok := ell.Elt.(*ast.Ident)
			if !ok {
				return "", fmt.Errorf("setup function %s has a non-primitive variadic parameter", fn.Name.Name)
			}
			conv, ok := argConversion(typ.Name, "goiArg", bridgePkg)
			if !ok {
				return "", fmt.Errorf("setup function %s has an unsupported variadic parameter %s", fn.Name.Name, typ.Name)
			}
			fmt.Fprintf(&setup, "var goiRest []%s\nfor _, goiArg := range goiArgs[%d:] {\ngoiRest = append(goiRest, %s)\n}\n", typ.Name, index, conv)
			args = append(args, "goiRest...")
			continue
		}
		typ, ok := field.Type.(*ast.Ident)
		if !ok {
			return "", fmt.Errorf("setup function %s has a non-primitive parameter", fn.Name.Name)
		}
		conv, ok := argConversion(typ.Name, fmt.Sprintf("goiArgs[%d]", index), bridgePkg)
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
	var b strings.Builder
	fmt.Fprintf(&b, "case %q:\n", fn.Name.Name)
	b.WriteString(setup.String())
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		fmt.Fprintf(&b, "return %s, nil\n", call)
	} else {
		fmt.Fprintf(&b, "%s\nreturn nil, nil\n", call)
	}
	return b.String(), nil
}

func argConversion(name, expr, bridgePkg string) (string, bool) {
	switch name {
	case "string":
		return bridgePkg + ".AsString(" + expr + ")", true
	case "bool":
		return bridgePkg + ".AsBool(" + expr + ")", true
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint64", "uintptr", "byte", "rune":
		return bridgePkg + ".AsInt(" + expr + ")", true
	case "float32", "float64":
		return bridgePkg + ".AsFloat(" + expr + ")", true
	case "any":
		return expr, true
	}
	return "", false
}
