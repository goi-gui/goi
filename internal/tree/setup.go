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
func (t *Tree) setupScope(src string) SetupScope {
	if t == nil || t.setup == nil {
		return nil
	}
	return t.setup(src)
}

// setupValue resolves a setup variable. A struct keeps its shape so a later
// field access can read it; scalars use the same normalization as bridge data.
func (t *Tree) setupValue(src, name string) (any, bool) {
	sc := t.setupScope(src)
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
func (t *Tree) setupHasFunc(src, name string) bool {
	sc := t.setupScope(src)
	return sc != nil && sc.HasFunc(name)
}

// setupCall invokes a setup function. found is false when the name is not a
// function, so the expression engine can try its builtins.
func (t *Tree) setupCall(src, name string, args []any) (any, bool) {
	sc := t.setupScope(src)
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

func nodeSource(b *NodeBase) string {
	if b == nil || b.AST == nil {
		return ""
	}
	return b.AST.Src
}
