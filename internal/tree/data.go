package tree

import (
	"reflect"
	"strings"

	"github.com/ksckaan1/goi/internal/syntax"
)

// scope binds an {#each} loop variable to one item. Scopes chain, so a nested
// block sees its own loop variable first and the outer one through parent.
type scope struct {
	name  string
	value any
	// parent is the enclosing loop scope, or nil at the top.
	parent *scope
}

// eachBlock is one {#each}: the list it walks, the arm it repeats and the nodes
// the last fill produced.
type eachBlock struct {
	src   string // the data variable name, without the `data.` prefix
	varN  string // loop variable
	arm   []syntax.Member
	empty []syntax.Member // the {:else} arm, nil when there is none
	// parent is the element the arm members are built under.
	parent *NodeBase
	scope  *scope
	hooks  *Hooks
	nodes  []*NodeBase
}

// SetData stores a value for `data.name` and rebuilds the blocks that walk it.
// A design reads it through `data.name`, a condition or an expression, so a set
// reaches the screen on the next frame.
//
// The value of a list variable is any slice; its elements may be structs (fields
// are matched by `json` tag, then by name, ignoring case) or map[string]any.
func (t *Tree) SetData(name string, v any) {
	if t == nil {
		return
	}
	if t.vars == nil {
		t.vars = map[string]any{}
	}
	t.vars[name] = v
	// A fill can register inner blocks and drop others, so walk a snapshot.
	for _, eb := range append([]*eachBlock(nil), t.eachs...) {
		if eb.src == name && t.live(eb.parent) {
			t.fillEach(eb)
		}
	}
	if t.Root != nil {
		t.Root.mark(DirtyLayout)
	}
}

// Data returns the current value of a data variable.
func (t *Tree) Data(name string) (any, bool) {
	v, ok := t.vars[name]
	return v, ok
}

// OnEach registers fn for every row built from `data.name`. The first element of
// the row arm is passed along with the item, so an app can attach a handler to
// the row it just made; the hook runs again for each rebuilt row.
func (t *Tree) OnEach(data string, fn func(Node, any)) {
	if fn == nil {
		return
	}
	if t.eachHooks == nil {
		t.eachHooks = map[string][]func(Node, any){}
	}
	t.eachHooks[data] = append(t.eachHooks[data], fn)
	// Rows that already exist are hooked now, so registering after the first
	// fill still works.
	for _, eb := range append([]*eachBlock(nil), t.eachs...) {
		if eb.src == data && t.live(eb.parent) {
			t.fillEach(eb)
		}
	}
}

// buildEach registers an {#each} and fills it from the data it names.
func (t *Tree) buildEach(blk *syntax.Block, parent *NodeBase, hooks *Hooks) error {
	eb := &eachBlock{
		src:    blk.Source,
		varN:   blk.Var,
		parent: parent,
		scope:  t.scope,
		hooks:  hooks,
	}
	if len(blk.Branches) > 0 {
		eb.arm = blk.Branches[0].Members
	}
	if len(blk.Branches) > 1 {
		eb.empty = blk.Branches[1].Members
	}
	t.eachs = append(t.eachs, eb)
	t.fillEach(eb)
	return nil
}

// Err returns the first error a data fill hit and clears it. A design is
// validated before the tree is built, so this reports a shape the app disagrees
// with, not a design mistake.
func (t *Tree) Err() error {
	err := t.dataErr
	t.dataErr = nil
	return err
}

// setErr remembers a failed fill without stopping the frame.
func (t *Tree) setErr(err error) {
	if t.dataErr == nil {
		t.dataErr = err
	}
}

// fillEach replaces the block's nodes with one copy of the arm per item. An
// empty list builds the {:else} arm instead.
func (t *Tree) fillEach(eb *eachBlock) {
	if !t.live(eb.parent) {
		return // its subtree is gone; prune drops the registration
	}
	for _, n := range eb.nodes {
		t.Remove(n.node)
	}
	eb.nodes = nil

	items, ok := sliceItems(t.vars[eb.src])
	if !ok || len(items) == 0 {
		if len(eb.empty) > 0 {
			// The else arm is built once, so its ids are registered like any
			// other element's.
			nodes, err := t.buildInto(eb, eb.empty, eb.scope, false)
			if err != nil {
				t.setErr(err)
			}
			eb.nodes = nodes
		}
		t.prune()
		eb.parent.mark(DirtyLayout)
		return
	}
	for _, item := range items {
		sc := &scope{name: eb.varN, value: item, parent: eb.scope}
		rows, err := t.buildInto(eb, eb.arm, sc, true)
		if err != nil {
			t.setErr(err)
			break
		}
		eb.nodes = append(eb.nodes, rows...)
		if len(rows) > 0 {
			for _, fn := range t.eachHooks[eb.src] {
				fn(rows[0].node, item)
			}
		}
	}
	t.prune()
	eb.parent.mark(DirtyLayout)
}

// buildInto builds one member list under the block's parent with sc in scope and
// returns the nodes it added.
func (t *Tree) buildInto(eb *eachBlock, members []syntax.Member, sc *scope, cloning bool) ([]*NodeBase, error) {
	before := len(eb.parent.kids)
	prevScope, prevCloning := t.scope, t.cloning
	t.scope = sc
	// A row's ids stay on the nodes like an Instantiate copy, so two rows of
	// the same arm do not collide; reach them with ui.Descendant.
	t.cloning = cloning
	err := t.buildMembers(members, eb.parent, eb.hooks, nil)
	t.scope, t.cloning = prevScope, prevCloning
	return append([]*NodeBase(nil), eb.parent.kids[before:]...), err
}

// live reports whether b is still attached to the tree.
func (t *Tree) live(b *NodeBase) bool {
	if t.Root == nil {
		return true // still building
	}
	for n := b; n != nil; n = n.parent {
		if n == &t.Root.NodeBase {
			return true
		}
	}
	return false
}

// prune drops registrations of nodes that are no longer in the tree, which is
// what a rebuilt {#each} leaves behind.
func (t *Tree) prune() {
	live := t.live
	keepExprs := t.exprNodes[:0]
	for _, b := range t.exprNodes {
		if live(b) {
			keepExprs = append(keepExprs, b)
		}
	}
	t.exprNodes = keepExprs

	keepConds := t.conds[:0]
	for _, cb := range t.conds {
		alive := len(cb.nodes) == 0
		for _, n := range cb.nodes {
			if live(n) {
				alive = true
				break
			}
		}
		if alive {
			keepConds = append(keepConds, cb)
		}
	}
	t.conds = keepConds

	keepResp := t.responsive[:0]
	for _, b := range t.responsive {
		if live(b) {
			keepResp = append(keepResp, b)
		}
	}
	t.responsive = keepResp

	keepColors := t.colorNodes[:0]
	for _, b := range t.colorNodes {
		if live(b) {
			keepColors = append(keepColors, b)
		}
	}
	t.colorNodes = keepColors

	// An {#each} inside a rebuilt row is gone with the row: a new one was built
	// while the rows were made.
	keepEachs := t.eachs[:0]
	for _, eb := range t.eachs {
		if live(eb.parent) {
			keepEachs = append(keepEachs, eb)
		}
	}
	t.eachs = keepEachs
}

// sliceItems turns a data value into its elements. A nil or non-slice value is
// an empty list, because the first frame runs before the app has fetched.
func sliceItems(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// dataValue reads one field of a data item: a struct field by `json` tag or
// name, or a map key. It returns a value an expression can use.
func dataValue(item any, field string) (any, bool) {
	if item == nil {
		return nil, false
	}
	if m, ok := item.(map[string]any); ok {
		return normalize(m[field])
	}
	rv := reflect.ValueOf(item)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.PkgPath != "" {
				continue
			}
			if dataFieldName(f) == field {
				return normalize(rv.Field(i).Interface())
			}
		}
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			v := rv.MapIndex(reflect.ValueOf(field))
			if v.IsValid() {
				return normalize(v.Interface())
			}
		}
	}
	return nil, false
}

// dataFieldName is the design-side name of a struct field: its `json` tag when
// it has one, otherwise the field name with a lower first letter.
func dataFieldName(f reflect.StructField) string {
	if tag := f.Tag.Get("json"); tag != "" {
		name := strings.Split(tag, ",")[0]
		if name != "" && name != "-" {
			return name
		}
	}
	if f.Name == "" {
		return ""
	}
	return strings.ToLower(f.Name[:1]) + f.Name[1:]
}

// normalize turns a Go value into the types the expression engine knows.
func normalize(v any) (any, bool) {
	switch x := v.(type) {
	case nil:
		return nil, false
	case string:
		return x, true
	case bool:
		return x, true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return x, true
	case uint64:
		return float64(x), true
	}
	// A named string or integer type, such as an enum the app declared.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.String(), true
	case reflect.Bool:
		return rv.Bool(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, false
		}
		return normalize(rv.Elem().Interface())
	}
	return nil, false
}

// scopeValue resolves `name.field` against the loop scopes of a node.
func scopeValue(sc *scope, name, field string) (any, bool) {
	for s := sc; s != nil; s = s.parent {
		if s.name != name {
			continue
		}
		return dataValue(s.value, field)
	}
	return nil, false
}

// normalizeData turns a data variable into the type the expression engine uses.
// A list is not a value; {#each} walks it.
func normalizeData(v any) (any, bool) {
	if _, isList := sliceItems(v); isList {
		return nil, false
	}
	return normalize(v)
}
