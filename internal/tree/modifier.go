package tree

import (
	"github.com/goi-gui/goi/internal/syntax"
)

// conditionalArm is one `value: condition` line of a conditional modifier.
type conditionalArm struct {
	value syntax.Value
	cond  syntax.Value
}

// conditionalProp is a property whose value comes from the first arm whose
// condition is true. It is evaluated with the expressions each frame, because a
// condition may read bridge data or a Go-set property.
type conditionalProp struct {
	name string
	arms []conditionalArm
}

// setModifier registers a responsive or conditional modifier on a property. It
// returns false when the property is unknown or the block is empty.
func (b *NodeBase) setModifier(name string, m *syntax.Modifier) bool {
	if m.Form == syntax.ModifierResponsive {
		entries := make([]syntax.MapEntry, len(m.Entries))
		for i, e := range m.Entries {
			entries[i] = syntax.MapEntry{Key: e.Key, KeyAt: e.KeyAt, Value: e.Value}
		}
		return b.setResponsiveEntries(name, entries)
	}
	b.properties()
	if _, ok := b.byName[name]; !ok {
		return false
	}
	cp := conditionalProp{name: name}
	for _, e := range m.Entries {
		cp.arms = append(cp.arms, conditionalArm{value: e.Value, cond: e.Cond})
	}
	if len(cp.arms) == 0 {
		return false
	}
	b.conds = append(b.conds, cp)
	// The conditional is an expression that writes a property, so the node
	// joins the same pass; exprListed keeps it from joining twice.
	if b.tree != nil && !b.exprListed {
		b.exprListed = true
		b.tree.exprNodes = append(b.tree.exprNodes, b)
	}
	return true
}

// applyConditionals writes every conditional property from the first arm whose
// condition holds. No arm matching leaves the property as it is, the way a
// switch without a matching case changes nothing.
func (b *NodeBase) applyConditionals() {
	for _, cp := range b.conds {
		if b.GoSet(cp.name) {
			continue
		}
		for _, arm := range cp.arms {
			c := evalCtx{tree: b.tree, sc: b.scope}
			got, has := c.eval(arm.cond, 0)
			truth, isBool := got.(bool)
			if !has || !isBool || !truth {
				continue
			}
			if v, ok := c.evalSpec(b, &exprSpec{name: cp.name, value: arm.value}, 0); ok {
				b.writeExpr(cp.name, v)
			}
			break
		}
	}
}
