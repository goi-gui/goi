package tree

import "github.com/ksckaan1/goui/internal/syntax"

// numericSpec is one expression-valued numeric property. The value stays a
// syntax expression so a reference can be re-read whenever a frame lays out.
type numericSpec struct {
	name  string
	value syntax.Value
}

// isExpression reports whether a value needs resolving rather than a plain
// literal write.
func isExpression(v syntax.Value) bool {
	switch v.(type) {
	case *syntax.Percent, *syntax.Ref, *syntax.Binary:
		return true
	}
	return false
}

// applyValue writes a property value from the .ui file, which may be an
// expression. Expressions are recorded for the next layout instead of stored
// in the property cell directly.
func (b *NodeBase) applyValue(name string, v syntax.Value) bool {
	if isExpression(v) {
		return b.setNumeric(name, v)
	}
	b.clearNumeric(name)
	return b.SetPropLiteral(name, v)
}

func (b *NodeBase) setNumeric(name string, v syntax.Value) bool {
	b.properties()
	if _, ok := b.byName[name]; !ok {
		return false
	}
	for j := range b.numerics {
		if b.numerics[j].name == name {
			b.numerics[j].value = v
			return true
		}
	}
	b.numerics = append(b.numerics, numericSpec{name: name, value: v})
	if b.tree != nil && !b.numericListed {
		b.numericListed = true
		b.tree.numericNodes = append(b.tree.numericNodes, b)
	}
	return true
}

func (b *NodeBase) clearNumeric(name string) {
	for i := range b.numerics {
		if b.numerics[i].name == name {
			b.numerics = append(b.numerics[:i], b.numerics[i+1:]...)
			return
		}
	}
}

func (b *NodeBase) numericSpecFor(name string) (numericSpec, bool) {
	for i := range b.numerics {
		if b.numerics[i].name == name {
			return b.numerics[i], true
		}
	}
	return numericSpec{}, false
}

// writeNumeric stores a resolved value without marking the node dirty: this is
// the build/layout path, not a Go property change.
func (b *NodeBase) writeNumeric(name string, v float64) {
	b.properties()
	if i, ok := b.byName[name]; ok {
		b.props[i].SetLiteral(&syntax.Number{Val: v})
	}
}

// ApplyNumeric resolves every expression that does not need the parent's size.
// The layout engine calls it once before a pass.
func (t *Tree) ApplyNumeric() {
	for _, b := range t.numericNodes {
		b.applyNumeric()
	}
}

// ApplyNumeric is the layout entry point; it delegates to the owning tree.
func (b *NodeBase) ApplyNumeric() {
	if b.tree != nil {
		b.tree.ApplyNumeric()
	}
}

func (b *NodeBase) applyNumeric() {
	for i := range b.numerics {
		spec := &b.numerics[i]
		if hasPercent(spec.value) {
			continue // ResolveSize needs the parent's content size
		}
		c := evalCtx{tree: b.tree, seen: map[refKey]bool{}}
		if v, ok := c.evalSpec(b, spec, 0); ok {
			b.writeNumeric(spec.name, v)
		}
	}
}

// ResolveSize resolves a percentage width or height against the space the
// parent offers. Layout calls it for each child before reading its size; a
// zero axis (auto measurement) leaves the previous value alone.
func (b *NodeBase) ResolveSize(availW, availH float64) {
	for i := range b.numerics {
		spec := &b.numerics[i]
		if !hasPercent(spec.value) {
			continue
		}
		scale := availW
		if spec.name == "height" {
			scale = availH
		} else if spec.name != "width" {
			continue
		}
		if scale <= 0 {
			continue
		}
		c := evalCtx{tree: b.tree, seen: map[refKey]bool{}}
		if v, ok := c.evalSpec(b, spec, scale); ok {
			b.writeNumeric(spec.name, v)
		}
	}
}

func hasPercent(v syntax.Value) bool {
	switch x := v.(type) {
	case *syntax.Percent:
		return true
	case *syntax.Binary:
		return hasPercent(x.Left) || hasPercent(x.Right)
	}
	return false
}

// refKey identifies one expression being evaluated, so a reference cycle stops
// instead of recursing forever.
type refKey struct {
	node *NodeBase
	prop string
}

type evalCtx struct {
	tree *Tree
	seen map[refKey]bool
}

func (c *evalCtx) evalSpec(b *NodeBase, spec *numericSpec, percent float64) (float64, bool) {
	key := refKey{node: b, prop: spec.name}
	if c.seen[key] {
		return 0, false
	}
	c.seen[key] = true
	defer delete(c.seen, key)
	return c.eval(spec.value, percent)
}

func (c *evalCtx) eval(v syntax.Value, percent float64) (float64, bool) {
	switch x := v.(type) {
	case *syntax.Number:
		return x.Val, true
	case *syntax.Percent:
		if percent <= 0 {
			return 0, false
		}
		return x.Val / 100 * percent, true
	case *syntax.Binary:
		left, ok := c.eval(x.Left, percent)
		if !ok {
			return 0, false
		}
		right, ok := c.eval(x.Right, percent)
		if !ok {
			return 0, false
		}
		switch x.Op {
		case "+":
			return left + right, true
		case "-":
			return left - right, true
		case "*":
			return left * right, true
		case "/":
			if right == 0 {
				return 0, false
			}
			return left / right, true
		}
	case *syntax.Ref:
		return c.evalRef(x)
	}
	return 0, false
}

func (c *evalCtx) evalRef(r *syntax.Ref) (float64, bool) {
	if c.tree == nil {
		return 0, false
	}
	n, ok := c.tree.ByID(r.ID)
	if !ok {
		return 0, false
	}
	b := n.Base()
	if spec, ok := b.numericSpecFor(r.Prop); ok {
		// A referenced percentage would depend on another parent, which the
		// validator rejects; be defensive if a stale tree slips through.
		if hasPercent(spec.value) {
			return 0, false
		}
		return c.evalSpec(b, &spec, 0)
	}
	v, ok := b.PropValue(r.Prop)
	f, ok := v.(float64)
	return f, ok
}
