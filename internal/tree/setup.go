package tree

import "fmt"

// SetupScope is the evaluated setup block of one design file.
// The interface lives here so the tree stays independent of the interpreter.
type SetupScope interface {
	Value(name string) (any, bool)
	HasFunc(name string) bool
	Call(name string, args []any) (any, bool, error)
}

// setupScope returns the setup block of src, or nil when the file has none.
func (t *Tree) setupScope(src, scope string) SetupScope {
	if t == nil || t.setup == nil {
		return nil
	}
	return t.setup(src, scope)
}

// setupValue resolves a setup variable. A struct keeps its shape so a later
// field access can read it; scalars use the same normalization as bridge data.
func (t *Tree) setupValue(src, scope, name string) (any, bool) {
	sc := t.setupScope(src, scope)
	if sc == nil {
		return nil, false
	}
	v, ok := sc.Value(name)
	if !ok {
		return nil, false
	}
	if n, ok := normalizeData(v); ok {
		return n, true
	}
	return v, true
}

// setupHasFunc reports whether a bare call names a setup function, so the
// evaluator does not evaluate its arguments a second time for a builtin.
func (t *Tree) setupHasFunc(src, scope, name string) bool {
	sc := t.setupScope(src, scope)
	return sc != nil && sc.HasFunc(name)
}

// setupCall invokes a setup function. found is false when the name is not a
// function, so the expression engine can try its builtins.
func (t *Tree) setupCall(src, scope, name string, args []any) (any, bool) {
	sc := t.setupScope(src, scope)
	if sc == nil {
		return nil, false
	}
	v, found, err := sc.Call(name, args)
	if !found {
		return nil, false
	}
	if err != nil {
		t.bridgeErr(err)
		return nil, true
	}
	if n, ok := normalizeData(v); ok {
		return n, true
	}
	return v, true
}

// CallBridge invokes a Go bridge handler and returns its value, which is how a
// setup function reaches a handler such as getFileName().
func (t *Tree) CallBridge(name string, args []any) (any, error) {
	if t == nil || t.bridge == nil {
		return nil, fmt.Errorf("goi: no bridge to call %q on", name)
	}
	return t.bridge.CallValue(name, args)
}

func nodeScope(b *NodeBase) string {
	if b == nil || b.AST == nil {
		return ""
	}
	return b.AST.Scope
}

func nodeSource(b *NodeBase) string {
	if b == nil || b.AST == nil {
		return ""
	}
	return b.AST.Src
}

// InstanceProps resolves the props a fragment instance passed, in the caller's
// context. A prop may name a loop variable or a bridge value, so it is read
// when setup asks rather than once at build.
func (t *Tree) InstanceProps(scope string) map[string]any {
	if t == nil || scope == "" {
		return nil
	}
	b := t.instances[scope]
	if b == nil || len(b.instanceProps) == 0 {
		return nil
	}
	out := make(map[string]any, len(b.instanceProps))
	for name, ip := range b.instanceProps {
		c := evalCtx{tree: t, sc: b.scope, src: ip.Src, scopeID: scope}
		// A bridge list is read raw, so setup can turn its struct rows into the
		// shape types the way it does for a bridge variable.
		if v, ok := c.rawValue(ip.Value); ok {
			out[name] = v
		}
	}
	return out
}
