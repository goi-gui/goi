package tree

import (
	"runtime"
	"strconv"

	"github.com/ksckaan1/goi/internal/syntax"
)

// exprSpec is one expression-valued property. The value stays a syntax
// expression so it can be re-read whenever a frame runs: a reference may point
// at a Go-set property or at the current theme.
type exprSpec struct {
	name  string
	value syntax.Value
}

// isExpression reports whether a value needs resolving rather than a plain
// literal write.
func isExpression(v syntax.Value) bool {
	switch v.(type) {
	case *syntax.Percent, *syntax.Ref, *syntax.Binary, *syntax.Unary, *syntax.Ternary:
		return true
	}
	return false
}

// applyValue writes a property value from the .goi file, which may be an
// expression. Expressions are recorded for the frame loop instead of stored in
// the property cell directly.
func (b *NodeBase) applyValue(name string, v syntax.Value) bool {
	if isExpression(v) {
		return b.setExpr(name, v)
	}
	b.clearExpr(name)
	return b.SetPropLiteral(name, v)
}

func (b *NodeBase) setExpr(name string, v syntax.Value) bool {
	b.properties()
	if _, ok := b.byName[name]; !ok {
		return false
	}
	for j := range b.exprs {
		if b.exprs[j].name == name {
			b.exprs[j].value = v
			return true
		}
	}
	b.exprs = append(b.exprs, exprSpec{name: name, value: v})
	if b.tree != nil && !b.exprListed {
		b.exprListed = true
		b.tree.exprNodes = append(b.tree.exprNodes, b)
	}
	return true
}

func (b *NodeBase) clearExpr(name string) {
	for i := range b.exprs {
		if b.exprs[i].name == name {
			b.exprs = append(b.exprs[:i], b.exprs[i+1:]...)
			return
		}
	}
}

func (b *NodeBase) exprSpecFor(name string) (exprSpec, bool) {
	for i := range b.exprs {
		if b.exprs[i].name == name {
			return b.exprs[i], true
		}
	}
	return exprSpec{}, false
}

// writeExpr stores a resolved value without going through Go's Prop.Set, then
// marks the node for the work the property needs. A value that did not change
// costs nothing, which is what keeps a per-frame re-evaluation cheap.
func (b *NodeBase) writeExpr(name string, v any) {
	// A number written into a string property is formatted, so `text: data.count`
	// reads naturally instead of silently doing nothing.
	if f, ok := v.(float64); ok {
		if cur, ok := b.PropValue(name); ok {
			if _, isString := cur.(string); isString {
				v = strconv.FormatFloat(f, 'f', -1, 64)
			}
		}
	}
	var lit syntax.Value
	switch x := v.(type) {
	case float64:
		lit = &syntax.Number{Val: x}
	case string:
		// Ident, not String: a string expression can feed an enum, a text
		// property or a color role, and all three accept an identifier.
		lit = &syntax.Ident{Val: x}
	case bool:
		lit = &syntax.Bool{Val: x}
	case uint32:
		lit = &syntax.Color{RGBA: x}
	default:
		return
	}
	if b.sameExprValue(name, v) {
		return
	}
	if b.SetPropLiteral(name, lit) {
		b.mark(b.PropDirty(name))
	}
}

func (b *NodeBase) sameExprValue(name string, v any) bool {
	if s, ok := v.(string); ok && b.colorRoleFor(name) == s {
		return true
	}
	cur, ok := b.PropValue(name)
	if !ok {
		return false
	}
	switch x := v.(type) {
	case float64:
		f, ok := cur.(float64)
		return ok && f == x
	case string:
		s, ok := cur.(string)
		return ok && s == x
	case bool:
		p, ok := cur.(bool)
		return ok && p == x
	case uint32:
		c, ok := cur.(uint32)
		return ok && c == x
	}
	return false
}

// ApplyExpressions resolves every expression that does not need the parent's
// size. The frame loop calls it before checking whether a frame is needed, so a
// Go property change that an expression reads also reaches the screen.
func (t *Tree) ApplyExpressions() {
	for _, b := range t.exprNodes {
		b.applyExpressions()
	}
	// Block conditions may read the same properties; both passes use the
	// values as they are now, so they run together.
	t.applyConds()
}

// ApplyExpressions is the layout entry point; it delegates to the owning tree.
func (b *NodeBase) ApplyExpressions() {
	if b.tree != nil {
		b.tree.ApplyExpressions()
	}
}

// SetViewport records the logical window size for `window.width` and
// `window.height` in expressions.
func (t *Tree) SetViewport(w, h float64) {
	t.viewW, t.viewH = w, h
}

// Mode is a light or dark scheme, the value a design reads as `app.mode` and an
// app reads from Window.Mode.
type Mode uint8

// Mode values. ModeUnknown is only reported for the desktop's preference when
// the platform does not say what it is.
const (
	ModeLight Mode = iota
	ModeDark
	ModeUnknown
)

// String returns the design-side name: light, dark, or unknown.
func (m Mode) String() string {
	switch m {
	case ModeDark:
		return "dark"
	case ModeUnknown:
		return "unknown"
	}
	return "light"
}

// Dark reports whether the mode is dark, which is what `app.dark` exposes.
func (m Mode) Dark() bool { return m == ModeDark }

// Mode reports the mood of the resolved scheme, which is what `app.mode`
// exposes to a design. Until the driver resolves a registered scheme, the tree
// falls back to the convention that a theme called "dark" is dark.
func (t *Tree) Mode() Mode {
	if t.modeSet {
		return t.mode
	}
	if t.Root != nil && t.Root.Theme.Get() == "dark" {
		return ModeDark
	}
	return ModeLight
}

// SetMode records the mood of the resolved scheme. The driver sets it whenever
// it picks a theme, so a custom dark theme still reads as dark.
func (t *Tree) SetMode(m Mode) {
	t.mode, t.modeSet = m, true
}

// Dark reports whether the resolved scheme is dark.
func (t *Tree) Dark() bool { return t.Mode().Dark() }

// SetDark is SetMode as a bool, for callers that only know the polarity.
func (t *Tree) SetDark(dark bool) {
	if dark {
		t.SetMode(ModeDark)
		return
	}
	t.SetMode(ModeLight)
}

// SystemMode reports the desktop's light/dark preference, or ModeUnknown when
// the platform does not report one.
func (t *Tree) SystemMode() Mode { return t.systemMode }

// SetSystemMode records the desktop preference, so a design can follow it with
// `theme: app.systemMode`.
func (t *Tree) SetSystemMode(m Mode) { t.systemMode = m }

// OS returns the operating system the app runs on, which is what `app.os`
// exposes to a design: linux, darwin, windows, freebsd.
func (t *Tree) OS() string { return runtime.GOOS }

// SetViewport is the layout entry point; it delegates to the owning tree.
func (b *NodeBase) SetViewport(w, h float64) {
	if b.tree != nil {
		b.tree.SetViewport(w, h)
	}
}

func (b *NodeBase) applyExpressions() {
	for i := range b.exprs {
		spec := &b.exprs[i]
		if hasPercent(spec.value) {
			continue // ResolveSize needs the parent's content size
		}
		c := evalCtx{tree: b.tree, sc: b.scope, seen: map[refKey]bool{}}
		if v, ok := c.evalSpec(b, spec, 0); ok {
			b.writeExpr(spec.name, v)
		}
	}
}

// ResolveSize resolves a percentage width or height against the space the
// parent offers. Layout calls it for each child before reading its size; a
// zero axis (auto measurement) leaves the previous value alone.
func (b *NodeBase) ResolveSize(availW, availH float64) {
	for i := range b.exprs {
		spec := &b.exprs[i]
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
		c := evalCtx{tree: b.tree, sc: b.scope, seen: map[refKey]bool{}}
		if v, ok := c.evalSpec(b, spec, scale); ok {
			if f, ok := v.(float64); ok {
				b.writeExpr(spec.name, f)
			}
		}
	}
}

func hasPercent(v syntax.Value) bool {
	switch x := v.(type) {
	case *syntax.Percent:
		return true
	case *syntax.Binary:
		return hasPercent(x.Left) || hasPercent(x.Right)
	case *syntax.Unary:
		return hasPercent(x.X)
	case *syntax.Ternary:
		return hasPercent(x.Cond) || hasPercent(x.Then) || hasPercent(x.Else)
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
	// sc is the loop scope of the node being evaluated, so `row.title` reads
	// the {#each} item.
	sc   *scope
	seen map[refKey]bool
}

func (c *evalCtx) evalSpec(b *NodeBase, spec *exprSpec, percent float64) (any, bool) {
	key := refKey{node: b, prop: spec.name}
	if c.seen[key] {
		return nil, false
	}
	c.seen[key] = true
	defer delete(c.seen, key)
	return c.eval(spec.value, percent)
}

func (c *evalCtx) eval(v syntax.Value, percent float64) (any, bool) {
	switch x := v.(type) {
	case *syntax.Number:
		return x.Val, true
	case *syntax.String:
		return x.Val, true
	case *syntax.Bool:
		return x.Val, true
	case *syntax.Color:
		return x.RGBA, true
	case *syntax.Ident:
		return x.Val, true
	case *syntax.Percent:
		if percent <= 0 {
			return nil, false
		}
		return x.Val / 100 * percent, true
	case *syntax.Unary:
		val, ok := c.eval(x.X, percent)
		if !ok {
			return nil, false
		}
		switch x.Op {
		case "!":
			b, ok := val.(bool)
			return !b, ok
		case "-":
			f, ok := val.(float64)
			return -f, ok
		}
		return nil, false
	case *syntax.Ternary:
		cond, ok := c.eval(x.Cond, percent)
		if !ok {
			return nil, false
		}
		b, ok := cond.(bool)
		if !ok {
			return nil, false
		}
		if b {
			return c.eval(x.Then, percent)
		}
		return c.eval(x.Else, percent)
	case *syntax.Binary:
		return c.evalBinary(x, percent)
	case *syntax.Ref:
		return c.evalRef(x)
	}
	return nil, false
}

func (c *evalCtx) evalBinary(x *syntax.Binary, percent float64) (any, bool) {
	left, ok := c.eval(x.Left, percent)
	if !ok {
		return nil, false
	}
	if x.Op == "&&" || x.Op == "||" {
		lb, ok := left.(bool)
		if !ok {
			return nil, false
		}
		// Short-circuit: the right side may divide by zero or read a prop that
		// only makes sense when the guard holds.
		if x.Op == "&&" && !lb {
			return false, true
		}
		if x.Op == "||" && lb {
			return true, true
		}
		right, ok := c.eval(x.Right, percent)
		if !ok {
			return nil, false
		}
		rb, ok := right.(bool)
		return rb, ok
	}
	right, ok := c.eval(x.Right, percent)
	if !ok {
		return nil, false
	}
	switch x.Op {
	case "==":
		return valuesEqual(left, right), true
	case "!=":
		return !valuesEqual(left, right), true
	case "<", "<=", ">", ">=":
		return compareValues(x.Op, left, right)
	case "+":
		if a, ok := left.(string); ok {
			b, ok := right.(string)
			return a + b, ok
		}
		return arith(x.Op, left, right)
	case "-", "*", "/":
		return arith(x.Op, left, right)
	}
	return nil, false
}

func valuesEqual(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case uint32:
		y, ok := b.(uint32)
		return ok && x == y
	}
	return false
}

func compareValues(op string, a, b any) (any, bool) {
	if x, ok := a.(string); ok {
		y, ok := b.(string)
		if !ok {
			return nil, false
		}
		switch op {
		case "<":
			return x < y, true
		case "<=":
			return x <= y, true
		case ">":
			return x > y, true
		default:
			return x >= y, true
		}
	}
	x, ok := a.(float64)
	if !ok {
		return nil, false
	}
	y, ok := b.(float64)
	if !ok {
		return nil, false
	}
	switch op {
	case "<":
		return x < y, true
	case "<=":
		return x <= y, true
	case ">":
		return x > y, true
	default:
		return x >= y, true
	}
}

func arith(op string, a, b any) (any, bool) {
	x, ok := a.(float64)
	if !ok {
		return nil, false
	}
	y, ok := b.(float64)
	if !ok {
		return nil, false
	}
	switch op {
	case "+":
		return x + y, true
	case "-":
		return x - y, true
	case "*":
		return x * y, true
	case "/":
		if y == 0 {
			return nil, false
		}
		return x / y, true
	}
	return nil, false
}

func (c *evalCtx) evalRef(r *syntax.Ref) (any, bool) {
	if c.tree == nil {
		return nil, false
	}
	// Two reserved namespaces: the app's theme and the window's size.
	switch r.ID {
	case "app":
		switch r.Prop {
		case "theme":
			return c.tree.Root.Theme.Get(), true
		case "mode":
			// The name, not the type: an enum or a color role can read it, the
			// same way `app.theme` feeds a string property.
			return c.tree.Mode().String(), true
		case "systemMode":
			return c.tree.SystemMode().String(), true
		case "dark":
			return c.tree.Dark(), true
		case "os":
			return c.tree.OS(), true
		}
		return nil, false
	case "window":
		switch r.Prop {
		case "width":
			return c.tree.viewW, true
		case "height":
			return c.tree.viewH, true
		}
		return nil, false
	}
	// A loop variable and the `data` block are values, not nodes.
	if c.sc != nil {
		if v, ok := scopeValue(c.sc, r.ID, r.Prop); ok {
			return v, true
		}
	}
	if r.ID == "data" {
		if v, ok := c.tree.vars[r.Prop]; ok {
			return normalizeData(v)
		}
		return nil, false
	}
	n, ok := c.tree.ByID(r.ID)
	if !ok {
		return nil, false
	}
	b := n.Base()
	if spec, ok := b.exprSpecFor(r.Prop); ok {
		// A referenced percentage would depend on another parent, which the
		// validator rejects; be defensive if a stale tree slips through.
		if hasPercent(spec.value) {
			return nil, false
		}
		return c.evalSpec(b, &spec, 0)
	}
	return b.PropValue(r.Prop)
}
