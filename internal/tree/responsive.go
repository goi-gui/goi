package tree

import (
	"math"
	"strings"
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
	// componentDefault is true when an unwritten colour property should fall
	// back to the component's own variant colour (a Button's filled background
	// is theme.primary even though the cell starts at zero).
	componentDefault bool
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
	if role := b.colorRoleFor(name); role != "" {
		mp.fallback = roleValue(role)
	} else if b.props[i].kind == propColor && !b.props[i].Written() {
		mp.componentDefault = true
	}
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

// clearModifier drops a property's responsive, conditional or state block, so a
// later Go write or a column resize is not overwritten by the next frame.
func (b *NodeBase) clearModifier(name string) {
	for i := range b.modifiers {
		if b.modifiers[i].name == name {
			b.modifiers = append(b.modifiers[:i], b.modifiers[i+1:]...)
			return
		}
	}
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
			v = b.modifierFallback(mp)
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

// modifierFallback is the value a block writes when none of its arms matches.
// A colour property that the design never wrote can borrow the component's own
// variant colour, so `background: state { hover: red }` starts from the filled
// Button's primary rather than transparent.
func (b *NodeBase) modifierFallback(mp *modifierProp) syntax.Value {
	if mp.componentDefault {
		if d, ok := b.node.(interface{ ModifierColorRole(string) string }); ok {
			if role := d.ModifierColorRole(mp.name); role != "" {
				return roleValue(role)
			}
		}
	}
	return mp.fallback
}

// roleValue turns a stored role string back into the reference a colour
// property writes.
func roleValue(role string) syntax.Value {
	ns, prop, ok := strings.Cut(role, ":")
	if !ok {
		return nil
	}
	if ns == "custom" {
		ns = "customColor"
	}
	return &syntax.Ref{ID: ns, Prop: prop}
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
		key := r.ID + ":" + r.Prop
		if r.ID == "customColor" {
			key = "custom:" + r.Prop
		}
		return b.colorRoleFor(mp.name) == key
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
	case Rotation:
		return &syntax.Rotate{Angle: x.Angle * 180 / math.Pi, Axis: axisName(x.Axis), Vector: x.Axis}
	case Scale:
		return &syntax.Scale{X: x.X, Y: x.Y, Z: x.Z}
	case []ShadowLayer:
		sh := &syntax.Shadow{}
		for _, l := range x {
			sl := syntax.ShadowLayer{X: l.X, Y: l.Y, Blur: l.Blur, Spread: l.Spread, Inset: l.Inset}
			switch {
			case l.Role != "":
				sl.Color = roleValue(l.Role)
			case l.Color != 0:
				sl.Color = &syntax.Color{RGBA: l.Color}
			}
			sh.Layers = append(sh.Layers, sl)
		}
		return sh
	case BackdropBlur:
		if x.Gradient != nil {
			return nil
		}
		return &syntax.Number{Val: x.Radius}
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
			rgba, ok := csscolor.ParseWithOpacity(x.Val)
			return ok && rgba == c
		}
	case *syntax.Rotate:
		c, ok := cur.(Rotation)
		if !ok {
			return false
		}
		r := rotationOf(x)
		return c == r || (c.IsZero() && r.IsZero())
	case *syntax.Scale:
		c, ok := cur.(Scale)
		return ok && c == scaleOf(x)
	case *syntax.Shadow:
		c, ok := cur.([]ShadowLayer)
		return ok && shadowEqual(c, shadowLayers(x))
	}
	if c, ok := cur.(BackdropBlur); ok {
		switch x := v.(type) {
		case *syntax.Number:
			return sameBackdropBlur(c, BackdropBlur{Radius: x.Val})
		case *syntax.String:
			g, ok := csscolor.ParseValueGradient(x.Val)
			return ok && sameBackdropBlur(c, BackdropBlur{Radius: g.Max(), Gradient: g})
		}
	}
	return false
}
