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

	"github.com/goi-gui/goi/internal/gosetup"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// Registry holds one interpreter per design file and fragment instance. A
// file's names are visible only to its own elements; every call of a fragment
// gets independent setup state.
type Registry struct {
	scopes map[string]*Scope
	// templates are one setup body per source file. A fragment instance gets a
	// fresh interpreter from its template.
	templates map[string]*template
	// baseUsed marks a source whose eager scope now belongs to its first
	// fragment instance. The next instance gets a clone.
	baseUsed map[string]bool
	// Live-tree callbacks, copied into scopes created after Bind.
	data    func(string) (any, bool)
	version func() uint64
	call    func(string, []any) (any, error)
}

type template struct {
	setup  syntax.Setup
	vars   []syntax.DataVar
	shapes []syntax.DataShape
}

func scopeKey(src, scope string) string { return src + "\x00" + scope }

// Scope is one file's interpreted setup declarations.
type Scope struct {
	i *interp.Interpreter
	// names are the declarations the body wrote, so an enum literal in a
	// property does not pay an interpreter lookup on every frame.
	names  map[string]bool
	values map[string]reflect.Value
	funcs  map[string]reflect.Value

	// The bridge callbacks are attached after the tree exists.
	data    func(string) (any, bool)
	version func() uint64
	call    func(string, []any) (any, error)
	// synced is the last DataVersion copied into the interpreted bridge.
	synced    uint64
	hasBridge bool
}

// New evaluates every setup block. A broken block is an error naming the file
// and the line inside it, so LoadWindow can report it like a design error.
func New(setups []syntax.Setup, vars []syntax.DataVar, shapes []syntax.DataShape) (*Registry, error) {
	r := &Registry{scopes: map[string]*Scope{}, templates: map[string]*template{}, baseUsed: map[string]bool{}}
	for _, st := range setups {
		if _, duplicate := r.templates[st.Src]; duplicate {
			return nil, fmt.Errorf("%s: setup may appear only once per file", st.Src)
		}
		r.templates[st.Src] = &template{setup: st, vars: vars, shapes: shapes}
		sc, err := newScope(st, vars, shapes)
		if err != nil {
			return nil, err
		}
		r.scopes[scopeKey(st.Src, "")] = sc
		if st.Src == "" {
			r.baseUsed[st.Src] = true
		}
	}
	return r, nil
}

func newScope(st syntax.Setup, vars []syntax.DataVar, shapes []syntax.DataShape) (*Scope, error) {
	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols)
	s := &Scope{i: i, hasBridge: len(vars) > 0, synced: ^uint64(0)}
	i.Use(interp.Exports{
		"goisetup/goisetup": {
			"Data":     reflect.ValueOf(s.bridgeData),
			"Call":     reflect.ValueOf(s.bridgeCall),
			"AsString": reflect.ValueOf(asString),
			"AsBool":   reflect.ValueOf(asBool),
			"AsInt":    reflect.ValueOf(asInt),
			"AsFloat":  reflect.ValueOf(asFloat),
			"AsUint32": reflect.ValueOf(asUint32),
		},
	})
	if _, err := i.Eval(gosetup.BridgeSource(st.Code, vars, shapes)); err != nil {
		return nil, evalError(st, err)
	}
	s.names, _ = declaredNames(st.Code)
	return s, nil
}

// Bind gives the scope the live tree: setup functions read its data and call
// its handlers. It runs after the tree is built, because setup names are needed
// while building it.
func (r *Registry) Bind(tr *tree.Tree) {
	if r == nil || tr == nil {
		return
	}
	r.data = tr.Data
	r.version = tr.DataVersion
	r.call = tr.CallBridge
	for _, sc := range r.scopes {
		r.bind(sc)
	}
}

func (r *Registry) bind(sc *Scope) {
	sc.data = r.data
	sc.version = r.version
	sc.call = r.call
	sc.synced = ^uint64(0)
	_ = sc.syncBridge()
}

func (s *Scope) bridgeData(name string) (any, bool) {
	if s == nil || s.data == nil {
		return nil, false
	}
	return s.data(name)
}

func (s *Scope) bridgeCall(name string, args ...any) (any, error) {
	if s == nil || s.call == nil {
		return nil, fmt.Errorf("setup bridge is not bound to a window yet")
	}
	v, err := s.call(name, args)
	_ = s.syncBridge()
	return v, err
}

func (s *Scope) syncBridge() error {
	if s == nil || !s.hasBridge || s.version == nil {
		return nil
	}
	v := s.version()
	if v == s.synced {
		return nil
	}
	s.synced = v
	if _, err := s.i.Eval("(&bridge).goiSync()"); err != nil {
		return fmt.Errorf("setup bridge: %w", err)
	}
	return nil
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(v)
	}
}

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	default:
		return v != nil
	}
}

func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case uint64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	}
	return 0
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case uint64:
		return float64(x)
	}
	return 0
}

func asUint32(v any) uint32 {
	switch x := v.(type) {
	case uint32:
		return x
	case int:
		return uint32(x)
	case int64:
		return uint32(x)
	case float64:
		return uint32(x)
	}
	return 0
}

// For returns the scope of one source file and fragment instance. An instance
// not seen before gets a fresh interpreter from the file's template.
func (r *Registry) For(src, scope string) tree.SetupScope {
	if r == nil {
		return nil
	}
	key := scopeKey(src, scope)
	if sc := r.scopes[key]; sc != nil {
		if scope == "" {
			r.baseUsed[src] = true
		}
		return sc
	}
	// The eager scope of a fragment file is not used by any entry element; let
	// its first instance take it instead of paying for an unused interpreter.
	if scope != "" && !r.baseUsed[src] {
		if sc := r.scopes[scopeKey(src, "")]; sc != nil {
			r.baseUsed[src] = true
			r.scopes[key] = sc
			return sc
		}
	}
	t := r.templates[src]
	if t == nil {
		return nil
	}
	sc, err := newScope(t.setup, t.vars, t.shapes)
	if err != nil {
		return nil
	}
	r.bind(sc)
	r.scopes[key] = sc
	return sc
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
	_ = s.syncBridge()
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
	if err := s.syncBridge(); err != nil {
		return nil, true, err
	}
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
	_ = s.syncBridge()
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
