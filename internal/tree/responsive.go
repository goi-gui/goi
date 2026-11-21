package tree

import (
	"time"

	"github.com/goi-gui/goi/internal/csscolor"
	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/syntax"
)

// modifierProp is one property whose value comes from a responsive,
// conditional or state block. The block is evaluated every frame, so nesting
// any of the three forms works: the value is found by walking the block.
type modifierProp struct {
	name     string
	root     syntax.Value
	fallback syntax.Value
	// applied is the block value last written, so re-evaluating an unchanged
	// expression is a no-op.
	applied syntax.Value
}

// setResponsive registers the older `{ base: 8, md: 16 }` brace map, which is
// the responsive form.
func (b *NodeBase) setResponsive(name string, m *syntax.Map) bool {
	mod := &syntax.Modifier{At: m.At, Form: syntax.ModifierResponsive}
	for _, e := range m.Entries {
		mod.Entries = append(mod.Entries, syntax.ModifierEntry{
			At: e.KeyAt, Key: e.Key, KeyAt: e.KeyAt, Value: e.Value,
		})
	}
	return b.setModifier(name, mod)
}

// setModifier registers a responsive, conditional or state modifier on a
// property. It returns false when the property is unknown or the block is
// empty.
func (b *NodeBase) setModifier(name string, m *syntax.Modifier) bool {
	b.properties()
	i, ok := b.byName[name]
	if !ok || len(m.Entries) == 0 {
		return false
	}
	mp := modifierProp{name: name, root: m, fallback: valueLiteral(b.props[i].Get())}
	for j := range b.modifiers {
		if b.modifiers[j].name == name {
			b.modifiers[j] = mp
			b.applyModifierBase(&b.modifiers[j])
			return true
		}
	}
	b.modifiers = append(b.modifiers, mp)
	// A responsive `base` is the value the property has before any frame runs,
	// which is what a headless build or a screenshot of the first layout sees.
	b.applyModifierBase(&b.modifiers[len(b.modifiers)-1])
	if b.tree != nil && !b.modifierListed {
		b.modifierListed = true
		b.tree.modifierNodes = append(b.tree.modifierNodes, b)
	}
	return true
}

// applyModifierBase writes the responsive base value at build time. A base that
// is itself a conditional or state block waits for the frame pass.
func (b *NodeBase) applyModifierBase(mp *modifierProp) {
	m, ok := mp.root.(*syntax.Modifier)
	if !ok || m.Form != syntax.ModifierResponsive {
		return
	}
	for _, e := range m.Entries {
		if e.Key != schema.BaseBreakpoint {
			continue
		}
		if _, nested := e.Value.(*syntax.Modifier); nested {
			return
		}
		b.applyValue(mp.name, e.Value)
		mp.applied = e.Value
		return
	}
}

// applyModifiers rewrites every modifier property in the node for the current
// viewport width and interaction state.
func (b *NodeBase) applyModifiers(width float64) {
	for i := range b.modifiers {
		mp := &b.modifiers[i]
		v := b.resolveModifier(mp.root, width)
		if v == nil {
			v = mp.fallback
		}
		if v == nil {
			continue
		}
		if b.modifierCurrent(mp, v) {
			// The fallback is already on the cell. Seed the transition from it
			// so the first matching arm animates instead of jumping.
			if mp.fallback == v {
				b.initTransitionCurrent(mp.name)
			}
			continue
		}
		if b.applyValue(mp.name, v) {
			mp.applied = v
			b.mark(b.PropDirty(mp.name))
		}
	}
}

// resolveModifier walks a modifier block and returns the value its current
// width, conditions and state select. A nil result means "restore the
// property's fallback".
func (b *NodeBase) resolveModifier(v syntax.Value, width float64) syntax.Value {
	if m, ok := v.(*syntax.Map); ok {
		var chosen syntax.Value
		best := -1.0
		for _, e := range m.Entries {
			min, ok := schema.BreakpointMin(e.Key)
			if !ok || min > width || min < best {
				continue
			}
			best, chosen = min, e.Value
		}
		if chosen == nil {
			return nil
		}
		return b.resolveModifier(chosen, width)
	}
	m, ok := v.(*syntax.Modifier)
	if !ok {
		return v
	}
	switch m.Form {
	case syntax.ModifierResponsive:
		var chosen syntax.Value
		best := -1.0
		for _, e := range m.Entries {
			min, ok := schema.BreakpointMin(e.Key)
			if !ok || min > width || min < best {
				continue
			}
			best, chosen = min, e.Value
		}
		if chosen == nil {
			return nil
		}
		return b.resolveModifier(chosen, width)
	case syntax.ModifierConditional:
		var fallback syntax.Value
		c := evalCtx{tree: b.tree, sc: b.scope}
		for _, e := range m.Entries {
			if e.Cond == nil {
				fallback = e.Value
				continue
			}
			got, ok := c.eval(e.Cond, 0)
			if truth, isBool := got.(bool); ok && isBool && truth {
				return b.resolveModifier(e.Value, width)
			}
		}
		if fallback == nil {
			return nil
		}
		return b.resolveModifier(fallback, width)
	case syntax.ModifierState:
		var fallback, matched syntax.Value
		for _, e := range m.Entries {
			if e.Key == "default" {
				fallback = e.Value
				continue
			}
			// The last matching state wins, so a design can order the block the
			// way CSS orders its rules: hover then active, disabled last.
			if b.stateOn(e.Key) {
				matched = e.Value
			}
		}
		if matched != nil {
			return b.resolveModifier(matched, width)
		}
		if fallback == nil {
			return nil
		}
		return b.resolveModifier(fallback, width)
	}
	return nil
}

// stateOn reports whether one state key is active on this node.
func (b *NodeBase) stateOn(name string) bool {
	c := componentOf(b)
	switch name {
	case "hover":
		return b.stateHover || (c != nil && c.Hover)
	case "active", "pressed":
		return b.statePressed || (c != nil && c.Pressed)
	case "focus":
		return b.stateFocused || (c != nil && c.Focused)
	}
	if c == nil {
		return name == "enabled"
	}
	switch name {
	case "disabled":
		return c.Disabled.Get()
	case "enabled":
		return !c.Disabled.Get()
	case "checked":
		return c.Checked.Get()
	case "unchecked":
		return !c.Checked.Get()
	case "selected":
		return c.Selected.Get()
	case "expanded":
		return c.Expanded.Get()
	case "collapsed":
		return !c.Expanded.Get()
	}
	return false
}

// componentOf returns the component a node embeds, or nil for a plain element.
func componentOf(b *NodeBase) *Component {
	if m, ok := b.node.(interface{ M3() *Component }); ok {
		return m.M3()
	}
	return nil
}

// modifierCurrent reports whether the property already holds v, so an unchanged
// state does not mark the node dirty every frame.
func (b *NodeBase) modifierCurrent(mp *modifierProp, v syntax.Value) bool {
	if r, ok := v.(*syntax.Ref); ok && (r.ID == "theme" || r.ID == "customColor") {
		return b.colorRoleFor(mp.name) == r.ID+":"+r.Prop
	}
	if isExpression(v) || b.isLoopVar(v) {
		return mp.applied == v
	}
	cur, ok := b.PropValue(mp.name)
	return ok && literalMatches(cur, v)
}

// ApplyBreakpoints rewrites every responsive property in the tree for the given
// logical window width, then resolves the conditional and state blocks that
// nest inside it. The driver calls it once per frame before layout.
func (t *Tree) ApplyBreakpoints(width float64) {
	t.modifierW = width
	t.applyModifiers()
}

// applyModifiers evaluates every modifier block. ApplyExpressions calls it a
// second time, after expression values are current, so a nested condition reads
// the same frame's values.
func (t *Tree) applyModifiers() {
	for _, b := range t.modifierNodes {
		b.applyModifiers(t.modifierW)
	}
}

// valueLiteral converts a typed property value back to a literal, so the value
// a node had before its map can be restored below the first breakpoint.
func valueLiteral(v any) syntax.Value {
	switch x := v.(type) {
	case string:
		return &syntax.Ident{Val: x}
	case float64:
		return &syntax.Number{Val: x}
	case bool:
		return &syntax.Bool{Val: x}
	case uint32:
		return &syntax.Color{RGBA: x}
	case time.Duration:
		return &syntax.Duration{Val: x}
	}
	return nil
}

// literalMatches reports whether v is already the property's value, mirroring
// what the NamedProp constructors accept.
func literalMatches(cur any, v syntax.Value) bool {
	switch x := v.(type) {
	case *syntax.Number:
		c, ok := cur.(float64)
		return ok && c == x.Val
	case *syntax.Bool:
		c, ok := cur.(bool)
		return ok && c == x.Val
	case *syntax.Color:
		c, ok := cur.(uint32)
		return ok && c == x.RGBA
	case *syntax.Ident:
		c, ok := cur.(string)
		return ok && c == x.Val
	case *syntax.Duration:
		c, ok := cur.(time.Duration)
		return ok && c == x.Val
	case *syntax.String:
		if c, ok := cur.(string); ok {
			return c == x.Val
		}
		if c, ok := cur.(uint32); ok {
			rgba, ok := csscolor.Parse(x.Val)
			return ok && rgba == c
		}
	}
	return false
}
