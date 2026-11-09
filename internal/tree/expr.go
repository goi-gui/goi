package tree

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/i18n"
	"github.com/goi-gui/goi/internal/syntax"
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
	switch x := v.(type) {
	case *syntax.Percent, *syntax.Ref, *syntax.Binary, *syntax.Unary, *syntax.Ternary:
		return true
	case *syntax.Call:
		// A builtin call such as formatf is a value; a handler call is only
		// legal as an event property and is bound there, not here.
		return x.Name != ""
	}
	return false
}

// applyValue writes a property value from the .goi file, which may be an
// expression. Expressions are recorded for the frame loop instead of stored in
// the property cell directly.
func (b *NodeBase) applyValue(name string, v syntax.Value) bool {
	// A colour reference is a literal for the cell, though it reads like a
	// reference: the resolver keeps it as a role and rewrites it per theme.
	if r, ok := v.(*syntax.Ref); ok && (r.ID == "theme" || r.ID == "customColor") {
		b.clearExpr(name)
		if b.SetPropLiteral(name, v) {
			b.mark(b.PropDirty(name))
			return true
		}
	}
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

// PercentWidth reports a declared percentage width as a fraction: `width: 10%`
// is 0.1. ResolveSize only runs for nodes the generic flow places, so a node
// that reads a child's width itself (a TableColumn) asks here instead.
func (b *NodeBase) PercentWidth() (float64, bool) {
	spec, ok := b.exprSpecFor("width")
	if !ok {
		return 0, false
	}
	p, ok := spec.value.(*syntax.Percent)
	if !ok {
		return 0, false
	}
	return p.Val / 100, true
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
	// The unchanged case is the common one on a repaint, and building the
	// literal would allocate, so check first.
	if b.sameExprValue(name, v) {
		return
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
	if b.SetPropLiteral(name, lit) {
		b.mark(b.PropDirty(name))
	}
}

func (b *NodeBase) sameExprValue(name string, v any) bool {
	if s, ok := v.(string); ok && b.colorRoleFor(name) == s {
		return true
	}
	b.properties()
	i, ok := b.byName[name]
	if !ok {
		return false
	}
	return b.props[i].Same(v)
}

// ApplyExpressions resolves every expression that does not need the parent's
// size. The frame loop calls it before checking whether a frame is needed, so a
// Go property change that an expression reads also reaches the screen.
func (t *Tree) ApplyExpressions() {
	// The cycle detector is a tree-owned stack, so it has to start empty and
	// not keep whatever depth the previous pass reached.
	t.evalSeen = t.evalSeen[:0]
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

// SystemLanguage returns the desktop's preferred language code ("tr", "en"), or
// "" when the platform does not report one. A design reads it as
// `app.systemLanguage`.
func (t *Tree) SystemLanguage() string { return t.systemLanguage }

// SetSystemLanguage records the desktop's preferred language.
func (t *Tree) SetSystemLanguage(lang string) { t.systemLanguage = lang }

// SetI18n installs the design's translation catalog. A nil bundle is the
// optional case: `t("key")` then renders the key itself.
func (t *Tree) SetI18n(b *i18n.Bundle) { t.i18n = b }

// SetLanguage chooses the language `t(...)` reads first. An empty code follows
// the desktop again, which is the default.
func (t *Tree) SetLanguage(code string) { t.language = code }

// Language reports the active language: the code SetLanguage chose, or the
// desktop's when none was set. `app.language` exposes it to a design.
func (t *Tree) Language() string {
	if t.language != "" {
		return t.language
	}
	return t.systemLanguage
}

// SetFallbackLanguage chooses the language a missing translation falls back to.
// An empty code follows the desktop, which is the default.
func (t *Tree) SetFallbackLanguage(code string) { t.fallbackLanguage = code }

// FallbackLanguage reports the fallback code, defaulting to the desktop's.
func (t *Tree) FallbackLanguage() string {
	if t.fallbackLanguage != "" {
		return t.fallbackLanguage
	}
	return t.systemLanguage
}

// Translate resolves a `t(...)` call: the active language, then the fallback,
// then the key itself. A design without an i18n folder therefore still shows
// the keys it wrote, which is what a developer wants before translating.
func (t *Tree) Translate(key string, args ...any) string {
	if t.i18n == nil {
		return key
	}
	return t.i18n.Translate(t.Language(), t.FallbackLanguage(), key, args...)
}

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
		if b.GoSet(spec.name) {
			// Go wrote this property, so the design no longer owns it: a theme
			// switch or a typed-in text must not be reverted every frame. Hot
			// reload keeps the same rule, and a changed .goi literal reclaims it.
			continue
		}
		if hasPercent(spec.value) {
			continue // ResolveSize needs the parent's content size
		}
		// A colour reference names a scheme role or an app-defined colour, which
		// the colour resolver owns; evaluating it as a value would write zero.
		if r, ok := spec.value.(*syntax.Ref); ok && (r.ID == "theme" || r.ID == "customColor") {
			continue
		}
		c := evalCtx{tree: b.tree, sc: b.scope}
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
		var scale float64
		switch spec.name {
		case "width":
			scale = availW
		case "height":
			scale = availH
		case "opacity":
			// A percentage of the 0..1 range, so 50% is 0.5.
			scale = 1
		default:
			continue
		}
		if scale <= 0 {
			continue
		}
		c := evalCtx{tree: b.tree, sc: b.scope}
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
	sc *scope
	// seen is the chain of references being evaluated, which is what stops a
	// cycle. The chain is shallow, so a slice scan beats a map and the tree
	// lends its scratch buffer: an expression pass allocates nothing.
	seen []refKey
}

func (c *evalCtx) evalSpec(b *NodeBase, spec *exprSpec, percent float64) (any, bool) {
	key := refKey{node: b, prop: spec.name}
	seen := &c.seen
	if c.tree != nil {
		seen = &c.tree.evalSeen
	}
	for _, s := range *seen {
		if s == key {
			return nil, false
		}
	}
	*seen = append(*seen, key)
	v, ok := c.eval(spec.value, percent)
	*seen = (*seen)[:len(*seen)-1]
	return v, ok
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
		// A bare name is a loop variable when one is in scope; otherwise it is
		// the literal word an enum or a string property reads.
		if v, ok := scopeVar(c.sc, x.Val); ok {
			return v, true
		}
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
	case *syntax.Call:
		return c.evalBuiltin(x, percent)
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
		// Two numbers add; anything else concatenates, so `"n: " + 3` and
		// `1 + 2` both mean what a reader expects.
		if _, ok := left.(float64); ok {
			if _, ok := right.(float64); ok {
				return arith(x.Op, left, right)
			}
		}
		a, aok := formatValue(left)
		b, bok := formatValue(right)
		if !aok || !bok {
			return nil, false
		}
		return a + b, true
	case "-", "*", "/":
		return arith(x.Op, left, right)
	}
	return nil, false
}

// evalBuiltin runs a Go builtin call. The pure ones are available in a design;
// the ones that allocate, mutate or touch the process (append, make, new, copy,
// delete, clear, close, panic, print, println, recover, complex, real, imag)
// have no meaning in a value that is re-read every frame.
func (c *evalCtx) evalBuiltin(call *syntax.Call, percent float64) (any, bool) {
	switch call.Name {
	case "formatf":
		return c.evalFormatf(call, percent)
	case "t":
		return c.evalTranslate(call, percent)
	case "len", "cap":
		return c.evalLenCall(call)
	case "min", "max":
		return c.evalMinMax(call, percent)
	}
	return nil, false
}

// evalFormatf formats its arguments with Go's fmt verbs.
func (c *evalCtx) evalFormatf(call *syntax.Call, percent float64) (any, bool) {
	if len(call.Args) == 0 {
		return "", true
	}
	format, ok := c.eval(call.Args[0], percent)
	if !ok {
		return nil, false
	}
	pattern, ok := format.(string)
	if !ok {
		return nil, false
	}
	verbs := integerVerbs(pattern)
	args := make([]any, 0, len(call.Args)-1)
	for i, a := range call.Args[1:] {
		v, ok := c.eval(a, percent)
		if !ok {
			return nil, false
		}
		// A design number is a float64; `%d` expects an integer, so the verb
		// decides the Go type it is handed.
		if i < len(verbs) && verbs[i] {
			if f, ok := v.(float64); ok {
				v = int64(f)
			}
		}
		args = append(args, formatArg(v))
	}
	return fmt.Sprintf(pattern, args...), true
}

// evalTranslate runs `t("key", args…)`: the first argument is the dotted key,
// the rest fill the entry's format verbs the way formatf fills its own.
func (c *evalCtx) evalTranslate(call *syntax.Call, percent float64) (any, bool) {
	if len(call.Args) == 0 || c.tree == nil {
		return "", true
	}
	key, ok := c.eval(call.Args[0], percent)
	if !ok {
		return nil, false
	}
	name, ok := key.(string)
	if !ok {
		return nil, false
	}
	if len(call.Args) == 1 {
		return c.tree.Translate(name), true
	}
	pattern := name
	if s, ok := c.tree.i18n.Lookup(c.tree.Language(), c.tree.FallbackLanguage(), name); ok {
		pattern = s
	}
	verbs := integerVerbs(pattern)
	args := make([]any, 0, len(call.Args)-1)
	for i, a := range call.Args[1:] {
		v, ok := c.eval(a, percent)
		if !ok {
			return nil, false
		}
		if i < len(verbs) && verbs[i] {
			if f, ok := v.(float64); ok {
				v = int64(f)
			}
		}
		args = append(args, formatArg(v))
	}
	return c.tree.Translate(name, args...), true
}

// evalLenCall is len and cap. A design list is not a value anywhere else, so the
// argument is read raw: a bridge variable, a props value or a loop variable.
func (c *evalCtx) evalLenCall(call *syntax.Call) (any, bool) {
	if len(call.Args) != 1 {
		return nil, false
	}
	v, ok := c.rawValue(call.Args[0])
	if !ok {
		return nil, false
	}
	switch x := v.(type) {
	case string:
		return float64(len([]rune(x))), true
	case []any:
		return float64(len(x)), true
	case map[string]any:
		return float64(len(x)), true
	}
	// Anything else is a single value: a scalar has no length, but a design
	// asking for one is more likely to mean its text.
	if items, isList := sliceItems(v); isList {
		return float64(len(items)), true
	}
	if m, isMap := mapItems(v); isMap {
		return float64(len(m)), true
	}
	if s, ok := formatValue(v); ok {
		return float64(len([]rune(s))), true
	}
	return nil, false
}

// evalMinMax is min and max, over numbers or strings as in Go.
func (c *evalCtx) evalMinMax(call *syntax.Call, percent float64) (any, bool) {
	if len(call.Args) == 0 {
		return nil, false
	}
	best, ok := c.eval(call.Args[0], percent)
	if !ok {
		return nil, false
	}
	for _, a := range call.Args[1:] {
		v, ok := c.eval(a, percent)
		if !ok {
			return nil, false
		}
		less, ok := lessValue(v, best)
		if !ok {
			return nil, false
		}
		if (call.Name == "min") == less {
			best = v
		}
	}
	switch best.(type) {
	case float64, string:
		return best, true
	}
	return nil, false
}

// lessValue reports whether a sorts before b: numbers compare numerically,
// strings lexically.
func lessValue(a, b any) (bool, bool) {
	if x, ok := a.(float64); ok {
		y, ok := b.(float64)
		return x < y, ok
	}
	if x, ok := a.(string); ok {
		y, ok := b.(string)
		return x < y, ok
	}
	return false, false
}

// rawValue evaluates a value without turning a list into "not a value", so len
// and cap can count one.
func (c *evalCtx) rawValue(v syntax.Value) (any, bool) {
	ref, ok := v.(*syntax.Ref)
	if !ok {
		return c.eval(v, 0)
	}
	switch ref.ID {
	case "bridge", "data":
		if c.tree != nil {
			if got, ok := c.tree.vars[ref.Prop]; ok {
				return got, true
			}
		}
		return nil, false
	case "props":
		return nil, false // props are substituted before the tree is built
	}
	if c.sc != nil {
		if got, ok := scopeValueAll(c.sc, ref.ID, ref.Prop); ok {
			return got, true
		}
	}
	return c.eval(v, 0)
}

// integerVerbs reports, for each verb in a format string, whether it expects an
// integer, so a design number can be converted before it reaches fmt.
func integerVerbs(pattern string) []bool {
	var out []bool
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '%' {
			continue
		}
		i++
		for i < len(pattern) && strings.ContainsRune("+-# 0.123456789", rune(pattern[i])) {
			i++
		}
		if i >= len(pattern) {
			break
		}
		switch pattern[i] {
		case '%':
			continue
		case 'd', 'b', 'o', 'O', 'x', 'X', 'c', 'U':
			out = append(out, true)
		default:
			out = append(out, false)
		}
	}
	return out
}

// formatArg turns a design value into something fmt can print: a color becomes
// #rrggbb, everything else keeps its own type.
func formatArg(v any) any {
	if c, ok := v.(uint32); ok {
		return fmt.Sprintf("#%06x", c>>8)
	}
	return v
}

// formatValue renders one value as text: what a string property and a `+`
// concatenation both show.
func formatValue(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(x), true
	case uint32:
		return fmt.Sprintf("#%06x", x>>8), true
	}
	return "", false
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
		case "systemLanguage":
			return c.tree.SystemLanguage(), true
		case "language":
			return c.tree.Language(), true
		case "dark":
			return c.tree.Dark(), true
		case "os":
			return c.tree.OS(), true
		}
		return nil, false
	case "window":
		root := c.tree.Root
		if root == nil {
			return nil, false
		}
		switch r.Prop {
		case "width":
			return c.tree.viewW, true
		case "height":
			return c.tree.viewH, true
		case "minimized":
			if min, _, _, known := root.PlatformState(); known {
				return min, true
			}
			return root.Minimized.Get(), true
		case "minimizable":
			return root.Minimizable.Get(), true
		case "maximized":
			if _, max, _, known := root.PlatformState(); known {
				return max, true
			}
			return root.Maximized.Get(), true
		case "maximizable":
			return root.Maximizable.Get(), true
		case "resizable":
			// Either spelling can turn resizing off.
			return root.Resizable.Get() && !root.DisableResize.Get(), true
		case "fullscreen":
			if _, _, full, known := root.PlatformState(); known {
				return full, true
			}
			return root.Fullscreen.Get(), true
		}
		return nil, false
	}
	// A loop variable and the `data` block are values, not nodes.
	if c.sc != nil {
		if v, ok := scopeValue(c.sc, r.ID, r.Prop); ok {
			return v, true
		}
	}
	if r.ID == "data" || r.ID == "bridge" {
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
