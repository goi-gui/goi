package tree

import (
	"reflect"
	"sort"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
)

// loopRow is one built iteration of a {#each} loop. nodes holds its direct
// children in source order; controls holds the continue and break statements
// with the number of nodes built before each one.
type loopRow struct {
	nodes    []*NodeBase
	controls []loopControl
	// dead marks a row the next fill replaced. A nested loop built in it keeps a
	// pointer to the row, so it can tell that its own empty result is stale.
	dead bool
}

// loopControl is one continue or break in a loop row. arm is the enclosing if
// arm, nil when the statement sits directly in the loop body.
type loopControl struct {
	after int
	brk   bool
	arm   *condBranch
}

// loopBuild is the current iteration while its members are built.
type loopBuild struct {
	parent *NodeBase
	row    *loopRow
}

// scope binds a loop variable to one value. Scopes chain, so a nested loop sees
// its own variables first and the outer ones through parent.
type scope struct {
	name  string
	value any
	// parent is the enclosing loop scope, or nil at the top.
	parent *scope
}

// eachBlock is one range loop: the value it walks, the arm it repeats and the
// nodes the last fill produced.
type eachBlock struct {
	// key and val are the loop variable names, "" when the design ignored them
	// with `_` or did not write them.
	key, val string
	// rng is the value the loop walks, evaluated on every fill so it follows a
	// bridge write.
	rng syntax.Value
	// listName is the bridge variable a loop walks, when it walks one, so
	// OnEach can hook the rows it builds.
	listName string
	src      string
	// setupScope is the fragment instance a prop-provided range resolves
	// against; empty means the node's own scope.
	setupScope string
	arm        []syntax.Member
	// parent is the element the arm members are built under, and at is the
	// slot in its child list where the loop was declared. A refill inserts the
	// new rows there, not at the end, so a later sibling (a map marker, say)
	// keeps painting above the rows.
	parent *NodeBase
	at     int
	scope  *scope
	hooks  *Hooks
	nodes  []*NodeBase
	// owner is the enclosing loop row, nil for a top-level loop. It is how an
	// empty nested loop knows its row has gone away even though parent is shared.
	owner *loopRow
	// rows are the rows the last fill produced, so the next fill can mark them
	// dead for their nested loops.
	rows []*loopRow
	// dead marks a nested loop whose parent row was removed. A SetData snapshot
	// can still name it after an outer fill pruned it, so fillEach must skip it.
	dead bool
}

// DataValues returns every value SetData has stored. Hot reload passes them
// into the next build so the design keeps its state.
func (t *Tree) DataValues() map[string]any {
	if t == nil {
		return nil
	}
	return t.vars
}

// DataVersion increments on every SetData, so a setup scope can refresh only
// when the bridge values actually changed.
func (t *Tree) DataVersion() uint64 {
	if t == nil {
		return 0
	}
	return t.dataVersion
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
	t.dataVersion++
	// A fill can register inner blocks and drop others, so walk a snapshot.
	for _, eb := range append([]*eachBlock(nil), t.eachs...) {
		if t.live(eb.parent) && eb.refers(name) {
			t.fillEach(eb)
		}
	}
	if t.Root != nil {
		t.Root.mark(DirtyLayout)
	}
}

// walks reports whether the loop's range expression reads the bridge variable
// name, so setting that variable rebuilds this loop. It looks inside a call, so
// `range len(bridge.rows)` follows the list it counts.
func (eb *eachBlock) walks(name string) bool {
	return eb.src == name || mentions(eb.rng, name)
}

// refers reports whether a SetData of name changes which rows this loop builds.
// The range is the obvious case. A continue or break also depends on the
// conditions around it, so only those conditions count: an ordinary expression
// in the body can update in place and keep its transition running.
func (eb *eachBlock) refers(name string) bool {
	if eb.walks(name) {
		return true
	}
	return controlMentions(eb.arm, name)
}

// controlMentions walks the loop body for a continue/break whose enclosing
// condition reads name. Nested loops own their controls and are skipped.
func controlMentions(members []syntax.Member, name string) bool {
	for _, m := range members {
		b, ok := m.(*syntax.Block)
		if !ok {
			continue
		}
		switch b.Kind {
		case syntax.BlockContinue, syntax.BlockBreak, syntax.BlockFor:
			continue
		case syntax.BlockSwitch:
			depends := mentions(b.Subject, name)
			for _, br := range b.Branches {
				for _, v := range br.Vals {
					if mentions(v, name) {
						depends = true
					}
				}
			}
			if depends && branchesHaveControl(b) {
				return true
			}
			for _, br := range b.Branches {
				if controlMentions(br.Members, name) {
					return true
				}
			}
		default:
			for _, br := range b.Branches {
				if br.Cond != nil && mentions(br.Cond, name) && hasControl(br.Members) {
					return true
				}
				if controlMentions(br.Members, name) {
					return true
				}
			}
		}
	}
	return false
}

// branchesHaveControl reports whether a switch arm contains a continue/break.
func branchesHaveControl(b *syntax.Block) bool {
	for _, br := range b.Branches {
		if hasControl(br.Members) {
			return true
		}
	}
	return false
}

// hasControl reports whether an arm contains a continue or break at this loop
// level. A nested loop's statements belong to that loop.
func hasControl(members []syntax.Member) bool {
	for _, m := range members {
		b, ok := m.(*syntax.Block)
		if !ok {
			continue
		}
		switch b.Kind {
		case syntax.BlockContinue, syntax.BlockBreak:
			return true
		case syntax.BlockFor:
			continue
		}
		for _, br := range b.Branches {
			if hasControl(br.Members) {
				return true
			}
		}
	}
	return false
}

// mentions reports whether v reads the bridge or data variable name, however
// deeply the reference is nested.
func mentions(v syntax.Value, name string) bool {
	switch x := v.(type) {
	case *syntax.Ref:
		return (x.ID == "bridge" || x.ID == "data") && x.Prop == name
	case *syntax.Call:
		for _, a := range x.Args {
			if mentions(a, name) {
				return true
			}
		}
	case *syntax.Binary:
		return mentions(x.Left, name) || mentions(x.Right, name)
	case *syntax.Unary:
		return mentions(x.X, name)
	case *syntax.Ternary:
		return mentions(x.Cond, name) || mentions(x.Then, name) || mentions(x.Else, name)
	case *syntax.List:
		for _, it := range x.Items {
			if mentions(it, name) {
				return true
			}
		}
	case *syntax.Map:
		for _, e := range x.Entries {
			if mentions(e.Value, name) {
				return true
			}
		}
	case *syntax.Modifier:
		for _, e := range x.Entries {
			if mentions(e.Value, name) || mentions(e.Cond, name) {
				return true
			}
		}
	}
	return false
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

// buildFor registers a range loop and fills it from the value it walks.
func (t *Tree) buildFor(blk *syntax.Block, parent *NodeBase, hooks *Hooks) error {
	var owner *loopRow
	if t.loop != nil {
		owner = t.loop.row
	}
	eb := &eachBlock{
		key:    blk.LoopKey(),
		val:    blk.LoopVal(),
		rng:    blk.Range,
		src:    blk.Src,
		parent: parent,
		at:     len(parent.kids),
		scope:  t.scope,
		hooks:  hooks,
		owner:  owner,
	}
	if blk.CallerSrc != "" {
		eb.src, eb.setupScope = blk.CallerSrc, blk.CallerScope
	} else {
		eb.setupScope = nodeScope(parent)
	}
	if len(blk.Branches) > 0 {
		eb.arm = blk.Branches[0].Members
	}
	if ref, ok := blk.Range.(*syntax.Ref); ok && (ref.ID == "bridge" || ref.ID == "data") {
		eb.listName = ref.Prop
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

// fillEach replaces the block's nodes with one copy of the arm per iteration.
// A loop is one node cheaper than Svelte's `{#each}`: there is no else arm, and
// Go's rules decide what the names mean.
func (t *Tree) fillEach(eb *eachBlock) {
	if eb.owner != nil && eb.owner.dead {
		eb.dead = true
	}
	if eb.dead || !t.live(eb.parent) {
		return // its row is gone; prune marked the registration
	}
	for _, row := range eb.rows {
		row.dead = true
	}
	eb.rows = nil
	for _, n := range eb.nodes {
		t.Remove(n.node)
	}
	eb.nodes = nil

	pairs, ok := t.rangePairs(eb)
	if !ok || len(pairs) == 0 {
		t.prune()
		eb.parent.mark(DirtyLayout)
		return
	}
	start := len(eb.parent.kids)
	for _, p := range pairs {
		sc := eb.scope
		if eb.val != "" {
			sc = &scope{name: eb.val, value: p.value, parent: sc}
		}
		if eb.key != "" {
			sc = &scope{name: eb.key, value: p.key, parent: sc}
		}
		row := &loopRow{}
		prevLoop := t.loop
		prevBuilding := t.building
		t.loop = &loopBuild{parent: eb.parent, row: row}
		t.building = true
		rows, err := t.buildInto(eb, eb.arm, sc, true)
		t.building = prevBuilding
		t.loop = prevLoop
		if err != nil {
			t.setErr(err)
			break
		}
		row.nodes = rows
		eb.rows = append(eb.rows, row)
		stop := t.applyLoopControls(row)
		eb.nodes = append(eb.nodes, row.nodes...)
		if len(row.nodes) > 0 {
			for _, fn := range t.eachHooks[eb.listName] {
				fn(row.nodes[0].node, p.value)
			}
		}
		if stop {
			break
		}
	}
	// buildInto appends to the parent; move the new rows back to the slot the
	// loop occupied, so a sibling declared after the loop still paints after it.
	k := eb.parent.kids
	if added := k[start:]; len(added) > 0 {
		at := min(eb.at, start)
		if at < start {
			out := make([]*NodeBase, 0, len(k))
			out = append(out, k[:at]...)
			out = append(out, added...)
			out = append(out, k[at:start]...)
			eb.parent.kids = out
		}
	}
	t.prune()
	eb.parent.mark(DirtyLayout)
}

// rangeValue resolves what a loop walks. A list is not a value to the expression
// engine (nothing may read it in a property), so the bridge namespace is read
// here directly.
func (t *Tree) rangeValue(eb *eachBlock) (any, bool) {
	if id, ok := eb.rng.(*syntax.Ident); ok {
		for s := eb.scope; s != nil; s = s.parent {
			if s.name == id.Val {
				return s.value, true
			}
		}
	}
	if ref, ok := eb.rng.(*syntax.Ref); ok {
		switch ref.ID {
		case "bridge", "data":
			if v, ok := t.vars[ref.Prop]; ok {
				return rawPath(v, ref.Path)
			}
			return nil, false
		case "props":
			return nil, false // props are substituted before the tree is built
		}
		if eb.scope != nil {
			if v, ok := scopeValueAll(eb.scope, ref.ID, ref.Prop); ok {
				return rawPath(v, ref.Path)
			}
		}
	}
	c := evalCtx{tree: t, sc: eb.scope, src: eb.src, scopeID: eb.setupScope}
	return c.eval(eb.rng, 0)
}

// dataPath walks the fields a reference names after its first one, so
// bridge.device.Name reads inside the shape row the bridge holds. The steps
// before the last stay raw, because a shape field is a map or a struct and only
// the final value is turned into an expression value.
func dataPath(item any, path []string) (any, bool) {
	for i, field := range path {
		if i == len(path)-1 {
			return dataValue(item, field)
		}
		v, ok := rawField(item, field)
		if !ok {
			return nil, false
		}
		item = v
	}
	return item, true
}

// rawPath is dataPath without normalizing, so a loop can range over a list a
// shape holds.
func rawPath(item any, path []string) (any, bool) {
	for _, field := range path {
		v, ok := rawField(item, field)
		if !ok {
			return nil, false
		}
		item = v
	}
	return item, true
}

// scopeValueAll is dataValue without normalizing, so a list field of a row can
// be ranged over.
func scopeValueAll(sc *scope, name, field string) (any, bool) {
	for s := sc; s != nil; s = s.parent {
		if s.name != name {
			continue
		}
		if v, ok := rawField(s.value, field); ok {
			return v, true
		}
	}
	return nil, false
}

// rawField reads one field without turning it into an expression value.
func rawField(item any, field string) (any, bool) {
	if item == nil {
		return nil, false
	}
	if m, ok := item.(map[string]any); ok {
		return mapField(m, field)
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
			if n := dataFieldName(f); n != "" && fieldMatches(n, field) {
				return rv.Field(i).Interface(), true
			}
		}
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, false
		}
		return reflectMapField(rv, field)
	}
	return nil, false
}

// rangePair is one iteration: the key (index or map key) and the value.
type rangePair struct {
	key, value any
}

// rangePairs evaluates what a loop walks and lays out its iterations, exactly as
// Go's range does: a slice gives index and element, a map gives key and value,
// and a number counts from zero.
func (t *Tree) rangePairs(eb *eachBlock) ([]rangePair, bool) {
	v, ok := t.rangeValue(eb)
	if !ok {
		return nil, false
	}
	if items, isList := sliceItems(v); isList {
		out := make([]rangePair, len(items))
		for i, item := range items {
			out[i] = rangePair{key: float64(i), value: item}
		}
		return out, true
	}
	if m, isMap := mapItems(v); isMap {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]rangePair, len(keys))
		for i, k := range keys {
			out[i] = rangePair{key: k, value: m[k]}
		}
		return out, true
	}
	if n, ok := normalize(v); ok {
		f, isNum := n.(float64)
		if !isNum {
			return nil, false
		}
		count := int(f)
		if count < 0 {
			count = 0
		}
		out := make([]rangePair, count)
		for i := range out {
			out[i] = rangePair{key: float64(i), value: float64(i)}
		}
		return out, true
	}
	return nil, false
}

// collect records a direct node of the current loop row, so a later continue
// or break knows where the row's remaining nodes start.
func (t *Tree) collect(n *NodeBase, parent *NodeBase) {
	if t.loop == nil || parent != t.loop.parent || n == nil {
		return
	}
	t.loop.row.nodes = append(t.loop.row.nodes, n)
}

// applyLoopControls applies the first reached continue or break in a row. It
// removes the nodes that follow the statement from the tree and reports
// whether the loop should stop building later iterations.
func (t *Tree) applyLoopControls(row *loopRow) bool {
	done := map[*condBranch]bool{}
	for _, c := range row.controls {
		if c.arm != nil && !t.branchOK(c.arm, done) {
			continue
		}
		keep := c.after
		if keep > len(row.nodes) {
			keep = len(row.nodes)
		}
		for _, n := range row.nodes[keep:] {
			t.Remove(n.node)
		}
		row.nodes = row.nodes[:keep]
		return c.brk
	}
	return false
}

// buildInto builds one member list under the block's parent with sc in scope and
// returns the nodes it added.
func (t *Tree) buildInto(eb *eachBlock, members []syntax.Member, sc *scope, cloning bool) ([]*NodeBase, error) {
	before := len(eb.parent.kids)
	prevScope, prevCloning := t.scope, t.cloning
	t.scope = sc
	// A row's ids stay on the nodes, so two rows of
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

	keepMods := t.modifierNodes[:0]
	for _, b := range t.modifierNodes {
		if live(b) {
			keepMods = append(keepMods, b)
		}
	}
	t.modifierNodes = keepMods

	keepColors := t.colorNodes[:0]
	for _, b := range t.colorNodes {
		if live(b) {
			keepColors = append(keepColors, b)
		}
	}
	t.colorNodes = keepColors

	keepTransitions := t.transitionNodes[:0]
	for _, b := range t.transitionNodes {
		if live(b) {
			keepTransitions = append(keepTransitions, b)
		}
	}
	t.transitionNodes = keepTransitions

	keepShadows := t.shadowNodes[:0]
	for _, b := range t.shadowNodes {
		if live(b) {
			keepShadows = append(keepShadows, b)
		}
	}
	t.shadowNodes = keepShadows

	// An {#each} inside a rebuilt row is gone with the row: a new one was built
	// while the rows were made. Its parent can be the shared element above the
	// outer loop, so checking only the parent would keep the stale inner loop and
	// let the next SetData fill it into the same parent a second time.
	keepEachs := t.eachs[:0]
	for _, eb := range t.eachs {
		alive := live(eb.parent) && (eb.owner == nil || !eb.owner.dead)
		if alive && len(eb.nodes) > 0 {
			alive = false
			for _, n := range eb.nodes {
				if live(n) {
					alive = true
					break
				}
			}
		}
		if alive {
			keepEachs = append(keepEachs, eb)
		} else {
			eb.dead = true
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

// mapItems turns a map value into its entries, keyed by a string.
func mapItems(v any) (map[string]any, bool) {
	if v == nil {
		return nil, false
	}
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
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
		v, ok := mapField(m, field)
		if !ok {
			return nil, false
		}
		return normalizeData(v)
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
			if fieldMatches(dataFieldName(f), field) {
				return normalizeData(rv.Field(i).Interface())
			}
		}
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, false
		}
		got, ok := reflectMapField(rv, field)
		if !ok {
			return nil, false
		}
		return normalizeData(got)
	}
	return nil, false
}

// mapField reads a map entry by name: exact first, then ignoring case, so
// `row.Title` reads the `"title"` a Go map holds.
func mapField(m map[string]any, field string) (any, bool) {
	if v, ok := m[field]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, field) {
			return v, true
		}
	}
	return nil, false
}

// reflectMapField reads a reflect map entry by name, exact then ignoring case.
func reflectMapField(rv reflect.Value, field string) (any, bool) {
	if v := rv.MapIndex(reflect.ValueOf(field)); v.IsValid() {
		return v.Interface(), true
	}
	iter := rv.MapRange()
	for iter.Next() {
		if strings.EqualFold(iter.Key().String(), field) {
			return iter.Value().Interface(), true
		}
	}
	return nil, false
}

// fieldMatches compares a design name with the name a Go value carries. The two
// differ only in the first letter's case often enough (`Title` against `title`)
// that the comparison ignores case.
func fieldMatches(got, want string) bool {
	return got != "" && strings.EqualFold(got, want)
}

// dataFieldName is the design-side name of a struct field: its `goi` tag when
// it has one, otherwise the field name with a lower first letter. A `-` tag
// hides the field.
func dataFieldName(f reflect.StructField) string {
	if tag, ok := f.Tag.Lookup("goi"); ok {
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			return ""
		}
		if name != "" {
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
	case reflect.Array:
		// A fixed array is a coordinate, [lon, lat], which a list reads as a
		// nested list. A slice is not normalized here: a loop walks it, and
		// normalizeData decides when it is a value.
		out := make([]any, rv.Len())
		for i := range out {
			n, ok := normalize(rv.Index(i).Interface())
			if !ok {
				return nil, false
			}
			out[i] = n
		}
		return out, true
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, false
		}
		return normalize(rv.Elem().Interface())
	}
	return nil, false
}

// scopeVar resolves a bare loop variable name to its value.
func scopeVar(sc *scope, name string) (any, bool) {
	for s := sc; s != nil; s = s.parent {
		if s.name == name {
			return normalize(s.value)
		}
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
// A list becomes a list value, so a list property can read it (a coordinate
// list from an API); the loop path does not use this, it walks the raw slice.
func normalizeData(v any) (any, bool) {
	if items, isList := sliceItems(v); isList {
		out := make([]any, len(items))
		for i, it := range items {
			// Nested lists are values too: a [][]float64 is a coordinate list
			// (a list of [lon, lat] pairs), not a list of scalars.
			n, ok := normalizeData(it)
			if !ok {
				return nil, false
			}
			out[i] = n
		}
		return out, true
	}
	return normalize(v)
}

// valueFits reports whether v can feed a bridge variable declared as t. The
// design type is a declaration rather than a Go type identity, so the check
// uses the value's Go kind and recurses into a list's element type.
func valueFits(v any, t syntax.Type) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch t.Kind {
	case syntax.TypeSlice:
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return false
		}
		return t.Elem == nil || rv.Len() == 0 || valueFits(rv.Index(0).Interface(), *t.Elem)
	case syntax.TypeMap:
		return rv.Kind() == reflect.Map
	case syntax.TypeFunc:
		return rv.Kind() == reflect.Func
	}
	switch t.Name {
	case "bool":
		return rv.Kind() == reflect.Bool
	case "string":
		return rv.Kind() == reflect.String
	case "color":
		return rv.Kind() == reflect.Uint32
	}
	if isNumericName(t.Name) {
		return isNumericKind(rv.Kind())
	}
	// A declared shape or a type built from a primitive: Go supplies a struct
	// or a map for the former and a scalar for the latter.
	switch rv.Kind() {
	case reflect.Struct, reflect.Map, reflect.String, reflect.Bool:
		return true
	}
	return isNumericKind(rv.Kind())
}

func isNumericKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// zeroRuntime is Go's zero value for a declared bridge type, so an unset
// variable still reads (`Count: 0`, `Name: ""`). A slice or a map starts empty;
// a handler has no value to fake.
func zeroRuntime(t syntax.Type) (any, bool) {
	switch t.Kind {
	case syntax.TypeSlice:
		return []any{}, true
	case syntax.TypeMap:
		return map[string]any{}, true
	case syntax.TypeFunc:
		return nil, false
	}
	switch t.Name {
	case "bool":
		return false, true
	case "string":
		return "", true
	case "color":
		return uint32(0), true
	}
	if isNumericName(t.Name) {
		return float64(0), true
	}
	return nil, false
}

// isNumericName reports whether a declared type is a Go number.
func isNumericName(name string) bool {
	switch name {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64", "byte", "rune":
		return true
	}
	return false
}
