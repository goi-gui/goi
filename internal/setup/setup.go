// Package setup runs a .goi file's `setup` block with Yaegi and exposes its
// declarations to the page's expressions.
package setup

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// Registry holds one interpreter per design file. A file's names are visible
// only to the elements written in that file, which the tree resolves by
// Element.Src.
type Registry struct {
	scopes map[string]*Scope
}

// Scope is one file's interpreted setup declarations.
type Scope struct {
	i *interp.Interpreter
	// names are the declarations the body wrote, so an enum literal in a
	// property does not pay an interpreter lookup on every frame.
	names  map[string]bool
	values map[string]reflect.Value
	funcs  map[string]reflect.Value
}

// New evaluates every setup block. A broken block is an error naming the file
// and the line inside it, so LoadWindow can report it like a design error.
func New(setups []syntax.Setup) (*Registry, error) {
	r := &Registry{scopes: map[string]*Scope{}}
	for _, st := range setups {
		if _, duplicate := r.scopes[st.Src]; duplicate {
			return nil, fmt.Errorf("%s: setup may appear only once per file", st.Src)
		}
		sc, err := newScope(st)
		if err != nil {
			return nil, err
		}
		r.scopes[st.Src] = sc
	}
	return r, nil
}

func newScope(st syntax.Setup) (*Scope, error) {
	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols)
	if _, err := i.Eval(st.Code); err != nil {
		return nil, evalError(st, err)
	}
	names, _ := declaredNames(st.Code)
	return &Scope{i: i, names: names}, nil
}

// For returns the scope of one source file, or nil when it declares none.
func (r *Registry) For(src string) tree.SetupScope {
	if r == nil {
		return nil
	}
	if sc := r.scopes[src]; sc != nil {
		return sc
	}
	return nil
}

// Value reads a setup variable. It is evaluated on every read so a function
// that changed it is visible; callers that read it every frame should keep the
// result in the design rather than call a mutating function.
func (s *Scope) Value(name string) (any, bool) {
	if s == nil || name == "" {
		return nil, false
	}
	if s.names != nil && !s.names[name] {
		return nil, false
	}
	if v, ok := s.values[name]; ok {
		return v.Interface(), true
	}
	v, err := s.i.Eval(name)
	if err != nil || !v.IsValid() || !v.CanInterface() || v.Kind() == reflect.Func {
		return nil, false
	}
	if s.values == nil {
		s.values = map[string]reflect.Value{}
	}
	s.values[name] = v
	return v.Interface(), true
}

// Call invokes a setup function. found is false when the name is not a
// function, which lets the expression engine fall back to its builtins.
func (s *Scope) Call(name string, args []any) (result any, found bool, err error) {
	fn, ok := s.fn(name)
	if !ok {
		return nil, false, nil
	}
	found = true
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("setup %s: %v", name, r)
		}
	}()
	ft := fn.Type()
	if ft.IsVariadic() {
		return nil, true, fmt.Errorf("setup %s: variadic functions are not supported", name)
	}
	if ft.NumIn() != len(args) {
		return nil, true, fmt.Errorf("setup %s takes %d argument(s), the design passes %d",
			name, ft.NumIn(), len(args))
	}
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		v, err := tree.BridgeArg(a, ft.In(i))
		if err != nil {
			return nil, true, fmt.Errorf("setup %s argument %d: %w", name, i+1, err)
		}
		in[i] = v
	}
	out := fn.Call(in)
	if n := len(out); n > 0 {
		last := out[n-1]
		if last.Type().Implements(errorType) {
			if !last.IsNil() {
				return nil, true, fmt.Errorf("setup %s: %w", name, last.Interface().(error))
			}
			out = out[:n-1]
		}
	}
	switch len(out) {
	case 0:
		return nil, true, nil
	case 1:
		if !out[0].IsValid() || !out[0].CanInterface() {
			return nil, true, nil
		}
		return out[0].Interface(), true, nil
	default:
		return nil, true, fmt.Errorf("setup %s returns %d values; one value and an optional error are supported",
			name, len(out))
	}
}

// HasFunc reports whether name is a setup function, without evaluating its
// arguments.
func (s *Scope) HasFunc(name string) bool {
	_, ok := s.fn(name)
	return ok
}

func (s *Scope) fn(name string) (reflect.Value, bool) {
	if s.names != nil && !s.names[name] {
		return reflect.Value{}, false
	}
	if v, ok := s.funcs[name]; ok {
		return v, true
	}
	v, err := s.i.Eval(name)
	if err != nil || !v.IsValid() || v.Kind() != reflect.Func || !v.CanInterface() {
		return reflect.Value{}, false
	}
	if s.funcs == nil {
		s.funcs = map[string]reflect.Value{}
	}
	s.funcs[name] = v
	return v, true
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// declaredNames lists the variables, constants, types and functions a setup
// body declares at package level. Types are harmless in the set: reading one as
// a value fails in the interpreter and falls back to a literal.
func declaredNames(code string) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "_.go", "package setup\n"+code, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				names[d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch spec := spec.(type) {
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						names[name.Name] = true
					}
				case *ast.TypeSpec:
					names[spec.Name.Name] = true
				}
			}
		}
	}
	return names, nil
}

// evalError turns Yaegi's synthetic _.go position into the file and line the
// setup block occupies in the design.
func evalError(st syntax.Setup, err error) error {
	msg := err.Error()
	const prefix = "_.go:"
	if strings.HasPrefix(msg, prefix) {
		rest := strings.TrimPrefix(msg, prefix)
		if i := strings.IndexByte(rest, ':'); i > 0 {
			if line, convErr := strconv.Atoi(rest[:i]); convErr == nil {
				return fmt.Errorf("%s:%d%s", st.Src, st.Line+line-1, rest[i:])
			}
		}
	}
	return fmt.Errorf("%s:%d: %v", st.Src, st.Line, err)
}
