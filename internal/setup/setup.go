// Package setup runs the `setup` blocks of a design with one shared Yaegi
// interpreter and exposes their declarations to the page's expressions.
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
)

// Registry holds the setup of one design: one shared interpreter, one Yaegi
// package per file, and one state struct per fragment instance. A file's names
// are visible only to its own elements; every call of a fragment gets
// independent setup state. A file is compiled on first use.
type Registry struct {
	m *machine
	// scopes holds the entry scope, lazy instance scopes and materialized
	// private scopes. A lazy scope is replaced by its private scope on first
	// stateful call.
	scopes map[string]tree.SetupScope
	// bases are the shared read-only scopes, one per setup file.
	bases map[string]*Scope
	// baseRefs counts lazy instances still reading their base.
	baseRefs map[string]int
	// programs are setup files that the shared closure compiler could handle.
	programs map[string]*program
	// templates are one setup body per source file for the fallback path.
	templates map[string]*template
	// err is the first compile error seen while resolving setup lazily.
	err error
	// tr resolves the props a fragment instance passed.
	tr *tree.Tree
	// hookSrc marks files that declare onMount or onUnmount.
	hookSrc map[string]bool
	// order is every instance created, so mounts and unmounts have a stable
	// order. queued and mounted track the lifecycle of one instance.
	order   []tree.SetupScope
	queued  map[tree.SetupScope]bool
	mounted map[tree.SetupScope]bool
	// visible tracks the fragments an if or a hidden ancestor currently shows.
	// visibilityKnown keeps a design without visibility hooks mounting all.
	visible         map[tree.SetupScope]bool
	visibilityKnown bool
	started         bool
	stopped         bool
	// post queues a mount on the UI goroutine; report surfaces a hook panic.
	post   func(func())
	report func(error)
	// Live-tree callbacks, copied into scopes created after Bind.
	data    func(string) (any, bool)
	version func() uint64
	call    func(string, []any) (any, error)
	set     func(string, any)
}

// errScope stands in for a file whose setup failed to compile. Its functions
// still report the error when an event calls one.
type errScope struct {
	p   *program
	err error
}

func (e *errScope) Value(string) (any, bool) { return nil, false }

func (e *errScope) HasFunc(name string) bool { return e.p.funcs[name] }

func (e *errScope) Call(string, []any) (any, bool, error) { return nil, true, e.err }

// lazyScope is a fragment instance that has not mutated its setup state yet.
// Reads share base; the first function call creates a private interpreter (or
// claims base when it is the last reader).
type lazyScope struct {
	r       *Registry
	src     string
	key     string
	base    *Scope
	private *Scope
	propGet func() map[string]any
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
	names     map[string]bool
	dataNames map[string]bool
	stateful  map[string]bool
	values    map[string]reflect.Value
	funcs     map[string]reflect.Value

	// The bridge callbacks are attached after the tree exists.
	data    func(string) (any, bool)
	version func() uint64
	call    func(string, []any) (any, error)
	// synced is the last DataVersion copied into the interpreted bridge.
	synced    uint64
	hasBridge bool
	// propGet resolves this scope's props; setProps writes the generated props
	// value before an interpreted call.
	writesBridge bool
	propGet      func() map[string]any
	setProps     reflect.Value
	// set writes a bridge assignment from setup back to the tree.
	set   func(string, any)
	flush reflect.Value
	last  map[string]any
}

// New registers every setup block and creates the shared machine. Compiling a
// block waits for its first use; Err reports a broken one after the tree has
// been built, and CompileAll checks them eagerly (goicheck).
func New(setups []syntax.Setup, vars []syntax.DataVar, shapes []syntax.DataShape) (*Registry, error) {
	r := &Registry{
		m:         newMachine(),
		scopes:    map[string]tree.SetupScope{},
		bases:     map[string]*Scope{},
		baseRefs:  map[string]int{},
		programs:  map[string]*program{},
		templates: map[string]*template{},
		hookSrc:   map[string]bool{},
		queued:    map[tree.SetupScope]bool{},
		mounted:   map[tree.SetupScope]bool{},
		visible:   map[tree.SetupScope]bool{},
	}
	seen := map[string]bool{}
	for _, st := range setups {
		if seen[st.Src] {
			return nil, fmt.Errorf("%s: setup may appear only once per file", st.Src)
		}
		seen[st.Src] = true
		if setupHooks(st.Code) {
			r.hookSrc[st.Src] = true
		}
		if p, err := newProgram(st, vars, shapes, len(r.programs), r.m); err == nil {
			r.programs[st.Src] = p
			continue
		}
		r.templates[st.Src] = &template{setup: st, vars: vars, shapes: shapes}
	}
	return r, nil
}

// Err is the first setup compile error from a lazily compiled file.
func (r *Registry) Err() error {
	if r == nil {
		return nil
	}
	return r.err
}

func (r *Registry) fail(err error) {
	if r != nil && r.err == nil {
		r.err = err
	}
}

// CompileAll evaluates every setup file now. goicheck uses it to report a
// broken block even when no element reads its names.
func (r *Registry) CompileAll() error {
	if r == nil {
		return nil
	}
	for _, p := range r.programs {
		if err := p.compile(); err != nil {
			r.fail(err)
			return err
		}
	}
	for src := range r.templates {
		if r.baseFor(src) == nil {
			return r.err
		}
	}
	return r.err
}

func newScope(st syntax.Setup, vars []syntax.DataVar, shapes []syntax.DataShape) (*Scope, error) {
	body, imports := gosetup.PrepareImports(st.Code)
	i := interp.New(interp.Options{})
	if err := useImports(i, imports, map[string]bool{}); err != nil {
		return nil, evalError(st, err)
	}
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
			"Field":    reflect.ValueOf(rowField),
		},
	})
	src := gosetup.ImportBlock(imports) + gosetup.BridgePrelude(vars, st.Props) + "\n//line " + st.Src + ":" + strconv.Itoa(st.Line) + "\n" +
		body + "\n//line _.go:1\n" + gosetup.BridgeDecls(vars, shapes, st.Props)
	if _, err := i.Eval(src); err != nil {
		return nil, evalError(st, err)
	}
	if len(st.Props) > 0 {
		if v, err := i.Eval("goiSetProps"); err == nil && v.IsValid() && v.Kind() == reflect.Func {
			s.setProps = v
		}
	}
	if v, err := i.Eval("goiFlush"); err == nil && v.IsValid() && v.Kind() == reflect.Func {
		s.flush = v
	}
	s.names, _ = declaredNames(body)
	s.dataNames, _ = valueNames(body)
	s.writesBridge = bridgeWrites(body)
	s.stateful, _ = statefulFuncs(body)
	return s, nil
}

// Bind gives the scope the live tree: setup functions read its data and call
// its handlers. It runs after the tree is built, because setup names are needed
// while building it.
func (r *Registry) Bind(tr *tree.Tree) {
	if r == nil || tr == nil {
		return
	}
	r.tr = tr
	r.data = tr.Data
	r.version = tr.DataVersion
	r.call = tr.CallBridge
	r.set = tr.SetData
	if r.m != nil {
		r.m.data = r.data
		r.m.version = r.version
		r.m.call = r.call
		r.m.set = r.set
	}
	for _, p := range r.programs {
		p.synced = ^uint64(0)
	}
	for _, sc := range r.scopes {
		switch x := sc.(type) {
		case *Scope:
			r.bind(x)
		case *lazyScope:
			x.bind()
		}
	}
}

func (r *Registry) bind(sc *Scope) {
	sc.data = r.data
	sc.version = r.version
	sc.call = r.call
	sc.set = r.set
	sc.synced = ^uint64(0)
	_ = sc.syncBridge()
}

func (s *Scope) bridgeValues() map[string]any {
	if s == nil || !s.flush.IsValid() {
		return nil
	}
	out := s.flush.Call(nil)
	if len(out) != 1 || !out[0].IsValid() || !out[0].CanInterface() {
		return nil
	}
	m, _ := out[0].Interface().(map[string]any)
	return m
}

func (s *Scope) flushLocal() {
	if s == nil || s.set == nil {
		return
	}
	after := s.bridgeValues()
	if after == nil {
		return
	}
	if s.last == nil {
		s.last = after
		return
	}
	for name, v := range after {
		if !reflect.DeepEqual(s.last[name], v) {
			s.set(name, v)
		}
	}
	s.last = after
}

func (s *Scope) syncProps() {
	if s == nil || !s.setProps.IsValid() || s.propGet == nil {
		return
	}
	m := s.propGet()
	if m == nil {
		m = map[string]any{}
	}
	s.setProps.Call([]reflect.Value{reflect.ValueOf(m)})
}

func (s *Scope) bridgeData(name string) (any, bool) {
	if s == nil || s.data == nil {
		return nil, false
	}
	v, ok := s.data(name)
	if !ok {
		return nil, false
	}
	return bridgeDataValue(v), true
}

func (s *Scope) bridgeCall(name string, args ...any) (any, error) {
	if s == nil || s.call == nil {
		return nil, fmt.Errorf("setup bridge is not bound to a window yet")
	}
	s.flushLocal()
	v, err := s.call(name, args)
	_ = s.syncBridge()
	return bridgeDataValue(v), err
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
	s.last = s.bridgeValues()
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
	case int8:
		return int(x)
	case int16:
		return int(x)
	case int32:
		return int(x)
	case int64:
		return int(x)
	case uint:
		return int(x)
	case uint8:
		return int(x)
	case uint16:
		return int(x)
	case uint32:
		return int(x)
	case uint64:
		return int(x)
	case uintptr:
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
	case int8:
		return float64(x)
	case int16:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case uint:
		return float64(x)
	case uint8:
		return float64(x)
	case uint16:
		return float64(x)
	case uint32:
		return float64(x)
	case uint64:
		return float64(x)
	}
	return 0
}

func asUint32(v any) uint32 {
	switch x := v.(type) {
	case uint8:
		return uint32(x)
	case uint16:
		return uint32(x)
	case uint32:
		return x
	case uint:
		return uint32(x)
	case uint64:
		return uint32(x)
	case int:
		return uint32(x)
	case int8:
		return uint32(x)
	case int16:
		return uint32(x)
	case int32:
		return uint32(x)
	case int64:
		return uint32(x)
	case float64:
		return uint32(x)
	case float32:
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
		return sc
	}
	if p := r.programs[src]; p != nil {
		if err := p.compile(); err != nil {
			r.fail(err)
			sc := &errScope{p: p, err: err}
			r.scopes[key] = sc
			return sc
		}
		inst := p.newInstance(r.propGetter(scope))
		if inst == nil {
			return nil
		}
		r.scopes[key] = inst
		if scope == "" {
			r.visible[inst] = true
		}
		r.track(inst)
		return inst
	}
	base := r.baseFor(src)
	if base == nil {
		return nil
	}
	var get func() map[string]any
	if t := r.templates[src]; t != nil && len(t.setup.Props) > 0 {
		get = r.propGetter(scope)
	}
	if scope == "" {
		base.propGet = get
		r.visible[base] = true
		r.track(base)
		return base
	}
	r.baseRefs[src]++
	l := &lazyScope{r: r, src: src, key: key, base: base, propGet: get}
	r.scopes[key] = l
	r.track(l)
	return l
}

func (r *Registry) baseFor(src string) *Scope {
	if sc := r.bases[src]; sc != nil {
		return sc
	}
	t := r.templates[src]
	if t == nil {
		return nil
	}
	sc, err := newScope(t.setup, t.vars, t.shapes)
	if err != nil {
		r.fail(err)
		return nil
	}
	r.bind(sc)
	r.bases[src] = sc
	r.baseRefs[src] = 0
	r.scopes[scopeKey(src, "")] = sc
	return sc
}

func (l *lazyScope) bind() {
	if l.private != nil {
		l.r.bind(l.private)
		return
	}
	if l.base != nil {
		l.r.bind(l.base)
	}
}

// current is the scope reads use. It never mutates a shared base.
func (l *lazyScope) current() *Scope {
	if l.private != nil {
		return l.private
	}
	if l.base != nil {
		return l.base
	}
	l.base = l.r.baseFor(l.src)
	return l.base
}

func (l *lazyScope) Value(name string) (any, bool) {
	if l.propGet != nil {
		sc, err := l.materialize()
		if err != nil {
			return nil, false
		}
		return sc.Value(name)
	}
	if sc := l.current(); sc != nil {
		return sc.Value(name)
	}
	return nil, false
}

func (l *lazyScope) HasFunc(name string) bool {
	if sc := l.current(); sc != nil {
		return sc.HasFunc(name)
	}
	return false
}

func (l *lazyScope) Call(name string, args []any) (any, bool, error) {
	// A function that neither reads nor writes setup state can run on the
	// shared base; there is nothing instance-specific to isolate. Props are
	// per instance, so a file that has them always uses its private state.
	if l.propGet == nil && l.private == nil && l.base != nil && l.base.sharedFunc(name) {
		return l.base.Call(name, args)
	}
	sc, err := l.materialize()
	if err != nil {
		return nil, true, err
	}
	return sc.Call(name, args)
}

func (l *lazyScope) materialize() (*Scope, error) {
	if l.private != nil {
		return l.private, nil
	}
	r := l.r
	if r.baseRefs[l.src] <= 1 {
		// The last reader takes the shared interpreter as its private state.
		r.baseRefs[l.src] = 0
		r.bases[l.src] = nil
		delete(r.scopes, scopeKey(l.src, ""))
		l.private = l.base
		l.private.propGet = l.propGet
		l.base = nil
		r.scopes[l.key] = l.private
		return l.private, nil
	}
	r.baseRefs[l.src]--
	t := r.templates[l.src]
	if t == nil {
		return nil, fmt.Errorf("%s: setup instance is no longer available", l.src)
	}
	sc, err := newScope(t.setup, t.vars, t.shapes)
	if err != nil {
		return nil, err
	}
	r.bind(sc)
	sc.propGet = l.propGet
	l.private = sc
	l.base = nil
	l.r.scopes[l.key] = sc
	if r.baseRefs[l.src] == 0 {
		r.bases[l.src] = nil
		delete(r.scopes, scopeKey(l.src, ""))
	}
	return sc, nil
}

// Value reads a setup variable. It is evaluated on every read so a function
// that changed it is visible; callers that read it every frame should keep the
// result in the design rather than call a mutating function.
func (s *Scope) Value(name string) (any, bool) {
	if s == nil || name == "" {
		return nil, false
	}
	if s.dataNames != nil && !s.dataNames[name] {
		return nil, false
	}
	if v, ok := s.values[name]; ok {
		return v.Interface(), true
	}
	s.syncProps()
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
	if isLifecycle(name) {
		return nil, false, nil
	}
	s.syncProps()
	if err := s.syncBridge(); err != nil {
		return nil, true, err
	}
	if s.writesBridge {
		defer s.flushLocal()
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

// sharedFunc reports whether a function uses no setup variable, directly or
// through another setup function, and may run on the shared base.
func (s *Scope) sharedFunc(name string) bool {
	return s != nil && s.stateful != nil && !s.stateful[name]
}

// HasFunc reports whether name is a setup function, without evaluating its
// arguments.
func (s *Scope) HasFunc(name string) bool {
	if isLifecycle(name) {
		return false
	}
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

// statefulFuncs marks the functions that read or write a package-level setup
// variable, directly or by calling another such function. A shared Lazy scope
// can call a pure function without copying the interpreter.
func statefulFuncs(code string) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "_.go", "package setup\n"+code, 0)
	if err != nil {
		return nil, err
	}
	vars := map[string]bool{}
	funcs := map[string]bool{}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range vs.Names {
						vars[name.Name] = true
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil {
				funcs[d.Name.Name] = true
			}
		}
	}
	stateful := map[string]bool{}
	calls := map[string][]string{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		direct := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if vars[x.Name] {
					direct = true
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok {
					calls[fn.Name.Name] = append(calls[fn.Name.Name], id.Name)
				}
			}
			return true
		})
		if direct {
			stateful[fn.Name.Name] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for name, called := range calls {
			if !stateful[name] {
				continue
			}
			for _, c := range called {
				if funcs[c] && !stateful[c] {
					stateful[c] = true
					changed = true
				}
			}
		}
	}
	return stateful, nil
}

// evalError turns Yaegi's synthetic _.go position into the file and line the
// setup block occupies in the design.
func evalError(st syntax.Setup, err error) error {
	msg := err.Error()
	// A generated //line directive already named the design file.
	if strings.HasPrefix(msg, st.Src+":") {
		return err
	}
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
