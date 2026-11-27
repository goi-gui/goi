package setup

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"

	"github.com/goi-gui/goi/internal/tree"
)

// A setup file may declare these to run when its instance appears and leaves.
// They are framework hooks, not design functions.
func isLifecycle(name string) bool { return name == "onMount" || name == "onUnmount" }

func setupHooks(code string) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "_.go", "package setup\n"+code, 0)
	if err != nil {
		return false
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && isLifecycle(fn.Name.Name) {
			return true
		}
	}
	return false
}

// lifecycle is implemented by every scope that can carry a mount or unmount
// hook. errScope does not: a file that failed to compile has nothing to run.
type lifecycle interface {
	hasHook(name string) bool
	callHook(name string) error
}

// Note creates the setup instance of a scope that is on screen, even when no
// expression reads its names, so a file with only onMount still mounts.
func (r *Registry) Note(src, scope string) {
	if r == nil || !r.hookSrc[src] {
		return
	}
	r.For(src, scope)
}

// Start runs onMount for every instance created so far and for instances
// created later. post queues work on the UI goroutine; the caller runs it after
// the first frame, so loading never waits for a mount hook.
func (r *Registry) Start(post func(func()), report func(error)) {
	if r == nil || r.started {
		return
	}
	r.started = true
	r.post = post
	r.report = report
	for _, sc := range r.order {
		r.schedule(sc)
	}
}

// Unmount runs onUnmount once for every started instance, newest first. It is
// idempotent and stays synchronous so cleanup finishes before teardown.
func (r *Registry) Unmount() {
	if r == nil || r.stopped {
		return
	}
	r.stopped = true
	for i := len(r.order) - 1; i >= 0; i-- {
		sc := r.order[i]
		if !r.mounted[sc] {
			continue
		}
		delete(r.mounted, sc)
		r.hook(sc, "onUnmount")
	}
	r.post = nil
}

// track records a new instance and mounts it at once when Start has already
// run, which is the case for an instance a data fill creates later.
func (r *Registry) track(sc tree.SetupScope) {
	if sc == nil {
		return
	}
	r.order = append(r.order, sc)
	if r.started {
		r.schedule(sc)
	}
}

func (r *Registry) schedule(sc tree.SetupScope) {
	if sc == nil || !r.started || r.stopped || r.mounted[sc] || r.queued[sc] {
		return
	}
	r.queued[sc] = true
	run := func() {
		delete(r.queued, sc)
		if r.stopped || r.mounted[sc] {
			return
		}
		r.mounted[sc] = true
		r.hook(sc, "onMount")
	}
	if r.post != nil {
		r.post(run)
		return
	}
	run()
}

func (r *Registry) hook(sc tree.SetupScope, name string) {
	h, ok := sc.(lifecycle)
	if !ok || !h.hasHook(name) {
		return
	}
	if err := h.callHook(name); err != nil && r.report != nil {
		r.report(err)
	}
}

func (e *errScope) hasHook(string) bool { return false }

func (e *errScope) callHook(string) error { return nil }

func (p *programInstance) field(name string) reflect.Value {
	switch name {
	case "onMount":
		return p.mount
	case "onUnmount":
		return p.unmount
	}
	return reflect.Value{}
}

func (p *programInstance) hasHook(name string) bool {
	f := p.field(name)
	return f.IsValid() && !f.IsNil()
}

func (p *programInstance) callHook(name string) error {
	f := p.field(name)
	if !f.IsValid() || f.IsNil() {
		return nil
	}
	if err := p.p.syncBridge(); err != nil {
		return err
	}
	var callErr error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				callErr = fmt.Errorf("setup %s: %v", name, rec)
			}
		}()
		f.Call(nil)
	}()
	return callErr
}

func (l *lazyScope) hasHook(name string) bool {
	if l.private != nil {
		return l.private.hasHook(name)
	}
	return l.base != nil && l.base.hasHook(name)
}

func (l *lazyScope) callHook(name string) error {
	sc, err := l.materialize()
	if err != nil {
		return err
	}
	return sc.callHook(name)
}

func (s *Scope) hasHook(name string) bool {
	fn, err := s.hookFn(name)
	return err == nil && fn.IsValid()
}

func (s *Scope) callHook(name string) error {
	fn, err := s.hookFn(name)
	if err != nil || !fn.IsValid() {
		return err
	}
	if err := s.syncBridge(); err != nil {
		return err
	}
	var callErr error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				callErr = fmt.Errorf("setup %s: %v", name, rec)
			}
		}()
		fn.Call(nil)
	}()
	return callErr
}

func (s *Scope) hookFn(name string) (reflect.Value, error) {
	if !isLifecycle(name) || s == nil || (s.names != nil && !s.names[name]) {
		return reflect.Value{}, nil
	}
	fn, ok := s.fn(name)
	if !ok {
		return reflect.Value{}, nil
	}
	t := fn.Type()
	if t.NumIn() != 0 || t.NumOut() != 0 {
		return reflect.Value{}, fmt.Errorf("setup %s must take no parameters and return nothing", name)
	}
	return fn, nil
}
