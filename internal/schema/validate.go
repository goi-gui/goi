package schema

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ksckaan1/goi/internal/m3"
	"github.com/ksckaan1/goi/internal/syntax"
)

// idPattern accepts a scoped id: an instance prefixes the ids it owns with its
// own name, so `card1.title` is a normal id after expansion.
var idPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*(\.[a-z][A-Za-z0-9_]*)*$`)

// Result is what validation learned that instantiation cannot recompute cheaply.
type Result struct {
	// IDs maps each element to its resolved id, for the elements that have one.
	IDs map[*syntax.Element]string
}

// Validate checks the AST against the schema, reporting every problem rather
// than stopping at the first. The AST itself is never modified.
//
// Any element may be the root: a fragment is a piece of a design, not a window.
// Use ValidateEntry for the file a program loads.
func Validate(f *syntax.File) (*Result, syntax.Diagnostics) {
	return validateFile(f, false)
}

// validateFile is the shared body. entry adds the rules that only hold for the file
// a program opens: a Window root, and no `props.name` left unexpanded.
func validateFile(f *syntax.File, entry bool) (*Result, syntax.Diagnostics) {
	if f == nil || f.Root == nil {
		return nil, syntax.Diagnostics{{Message: "the file has no root element"}}
	}
	v := &validator{
		file:  f,
		ids:   map[*syntax.Element]string{},
		elems: map[string]*syntax.Element{},
		seen:  map[string]syntax.Pos{},
		entry: entry,
	}
	if _, ok := Lookup(f.Root.Name); !ok {
		v.elementUnknown(f.Root)
	}
	v.checkData()
	v.checkProps()
	v.walk(f.Root, "")
	if v.diag.Empty() {
		v.walkRefs(f.Root)
	}
	if !v.diag.Empty() {
		return nil, v.diag
	}
	return &Result{IDs: v.ids}, nil
}

// ValidateEntry is Validate plus the rule that an entry point must be a Window,
// because that is what a program opens.
func ValidateEntry(f *syntax.File) (*Result, syntax.Diagnostics) {
	res, diag := validateFile(f, true)
	if f == nil || f.Root == nil || !diag.Empty() {
		return res, diag
	}
	if f.Root.Name != "Window" {
		return res, syntax.Diagnostics{{
			Pos:     f.Root.NameAt,
			End:     f.Root.Close,
			Src:     f.Root.Src,
			Message: fmt.Sprintf("the root element must be Window, not %s", f.Root.Name),
			Hint:    syntax.Suggest(f.Root.Name, RootNames()),
		}}
	}
	return res, nil
}

type validator struct {
	ids   map[*syntax.Element]string
	elems map[string]*syntax.Element
	seen  map[string]syntax.Pos
	diag  syntax.Diagnostics
	// src is the file the element being validated came from, so a diagnostic
	// lands in the right file after imports are expanded.
	src string
	// entry reports `props.name` refs, which are legal only in a fragment.
	entry bool
	// file supplies the data declarations an {#each} walks.
	file *syntax.File
	// scopes is the stack of loop variables in scope, innermost last.
	scopes []map[string]*syntax.DataShape
}

func (v *validator) add(at, end syntax.Pos, msg, hint string) {
	v.diag = append(v.diag, syntax.Diagnostic{Pos: at, End: end, Message: msg, Hint: hint, Src: v.src})
}

// addIn is add for a position that belongs to a specific file, which is the case
// when an instance overrides a fragment property.
func (v *validator) addIn(src string, at, end syntax.Pos, msg, hint string) {
	v.diag = append(v.diag, syntax.Diagnostic{Pos: at, End: end, Message: msg, Hint: hint, Src: src})
}

// scalarTypes are the value types a data field may use.
var scalarTypes = []string{"bool", "string", "number", "color"}

// checkData validates the `bridge` block (still spelled `data` in older files)
// and the row types it names.
func (v *validator) checkData() {
	v.src = ""
	seenShape := map[string]syntax.Pos{}
	for _, sh := range v.file.Shapes {
		if at, dup := seenShape[sh.Name]; dup {
			_ = at
			v.add(sh.NameAt, sh.NameAt+syntax.Pos(len(sh.Name)), fmt.Sprintf("duplicate type %q", sh.Name), "")
			continue
		}
		seenShape[sh.Name] = sh.NameAt
		seenField := map[string]bool{}
		for _, f := range sh.Fields {
			v.src = ""
			if seenField[f.Name] {
				v.add(f.NameAt, f.NameAt+syntax.Pos(len(f.Name)), fmt.Sprintf("%s repeats the field %q", sh.Name, f.Name), "")
			}
			seenField[f.Name] = true
			v.checkDataType(f.At, f.List, f.Type, f.Elem, "field "+sh.Name+"."+f.Name)
		}
	}
	seenVar := map[string]syntax.Pos{}
	for _, dv := range v.file.Vars {
		if _, dup := seenVar[dv.Name]; dup {
			v.add(dv.NameAt, dv.NameAt+syntax.Pos(len(dv.Name)), fmt.Sprintf("duplicate bridge variable %q", dv.Name), "")
			continue
		}
		seenVar[dv.Name] = dv.NameAt
		if dv.Name == "data" || dv.Name == "bridge" || dv.Name == "props" || dv.Name == "app" || dv.Name == "window" {
			v.add(dv.NameAt, dv.NameAt+syntax.Pos(len(dv.Name)), fmt.Sprintf("%q is a reserved name", dv.Name), "")
		}
		if dv.Fn {
			for _, a := range dv.Args {
				v.checkFuncArg(a)
			}
			continue
		}
		v.checkDataType(dv.At, dv.List, dv.Type, dv.Elem, "bridge "+dv.Name)
	}
}

// checkFuncArg accepts the scalar types a handler parameter may declare. A
// handler is a Go function, so a list or a shape would have no Go value to
// receive.
func (v *validator) checkFuncArg(a syntax.DataField) {
	v.src = ""
	for _, ok := range scalarTypes {
		if a.Type == ok {
			return
		}
	}
	v.add(a.At, a.At+syntax.Pos(len(a.Type)),
		fmt.Sprintf("handler parameter %q: %q is not a type; use bool, string, number or color", a.Name, a.Type),
		syntax.Suggest(a.Type, scalarTypes))
}

// checkDataType accepts a scalar type or a declared shape for a list.
func (v *validator) checkDataType(at syntax.Pos, list bool, typ, elem, what string) {
	v.src = ""
	end := at + syntax.Pos(len(typ))
	if list {
		if v.file.Shape(elem) == nil {
			v.add(at, end, fmt.Sprintf("%s: unknown shape %q", what, elem),
				syntax.Suggest(elem, shapeNames(v.file)))
		}
		return
	}
	for _, ok := range scalarTypes {
		if typ == ok {
			return
		}
	}
	v.add(at, end, fmt.Sprintf("%s: %q is not a type; use bool, string, number, color, [shape] or shape[]", what, typ),
		syntax.Suggest(typ, scalarTypes))
}

// shapeNames lists the declared shapes, for suggestions.
func shapeNames(f *syntax.File) []string {
	out := make([]string, 0, len(f.Shapes))
	for _, sh := range f.Shapes {
		out = append(out, sh.Name)
	}
	return out
}

// varNames lists the declared data variables, for suggestions.
func varNames(f *syntax.File) []string {
	out := make([]string, 0, len(f.Vars))
	for _, dv := range f.Vars {
		out = append(out, dv.Name)
	}
	return out
}

// lookupLoopVar returns the shape a loop variable holds, if it is in scope.
func (v *validator) lookupLoopVar(name string) *syntax.DataShape {
	for i := len(v.scopes) - 1; i >= 0; i-- {
		if sh, ok := v.scopes[i][name]; ok {
			return sh
		}
	}
	return nil
}

// fieldType maps a data type name onto the property type it can feed.
func fieldType(list bool, typ string) (ValueType, bool) {
	if list {
		return TypeString, false
	}
	switch typ {
	case "bool":
		return TypeBool, true
	case "string":
		return TypeString, true
	case "number":
		return TypeNumber, true
	case "color":
		return TypeColor, true
	}
	return TypeString, false
}

func (v *validator) elementUnknown(e *syntax.Element) {
	v.add(e.NameAt, e.NameAt+2, fmt.Sprintf("unknown element %q", e.Name), syntax.Suggest(e.Name, ElementNames()))
}

// walk validates one element and recurses into its children. parent is the
// enclosing element name, "" for the root.
func (v *validator) walk(e *syntax.Element, parent string) {
	v.src = e.Src
	def, ok := Lookup(e.Name)
	if !ok {
		v.elementUnknown(e)
		return // children of an unknown element cannot be meaningfully checked
	}
	if e.Name == "Positioned" && parent != "Stack" {
		v.add(e.NameAt, e.NameAt+syntax.Pos(len(e.Name)), "Positioned must be a direct child of Stack", "")
	}

	seen := map[string]syntax.Pos{}
	for _, p := range e.Props() {
		// A property may come from the file that instantiates this fragment, so
		// the position belongs to that file, not to the element's own.
		if p.Src != "" {
			v.src = p.Src
		}
		at := p.NameAt
		if _, dup := seen[p.Name]; dup {
			v.add(at, at+syntax.Pos(len(p.Name)), fmt.Sprintf("duplicate property %q", p.Name), "")
			continue
		}
		seen[p.Name] = at

		pd := def.Prop(p.Name)
		if pd == nil {
			if _, ok := Event(p.Name); ok && def.HasEvent(p.Name) {
				if _, ok := p.Value.(*syntax.Call); !ok {
					v.add(at, at+syntax.Pos(len(p.Name)),
						fmt.Sprintf("%s.%s expects a handler call such as %s: bridge.onOpen(...)", e.Name, p.Name, p.Name),
						"declare the handler in `bridge { ... }` and call it here; it runs only when the event fires")
				}
				continue
			}
			v.add(at, at+syntax.Pos(len(p.Name)),
				fmt.Sprintf("unknown property %q on %s", p.Name, e.Name), syntax.Suggest(p.Name, propNames(def)))
			continue
		}
		v.checkValue(e, p, pd)
		v.src = e.Src
	}

	if first := firstChild(e); first != nil && !def.Children {
		v.add(first.At, first.At+syntax.Pos(len(first.Name)), fmt.Sprintf("%s cannot contain child elements", e.Name), "")
	}

	for _, m := range e.Members {
		switch x := m.(type) {
		case *syntax.Element:
			v.walk(x, e.Name)
		case *syntax.Block:
			v.walkBlock(x, e.Name)
		}
	}
}

// childStart is where a child member begins, for a diagnostic range.
type childStart struct {
	At   syntax.Pos
	Name string
}

// firstChild returns the first child element or block of e, or nil.
func firstChild(e *syntax.Element) *childStart {
	for _, m := range e.Members {
		switch x := m.(type) {
		case *syntax.Element:
			return &childStart{At: x.NameAt, Name: x.Name}
		case *syntax.Block:
			return &childStart{At: x.At, Name: "{#if}"}
		}
	}
	return nil
}

// walkBlock checks one control block: the condition is a bool expression and the
// arms hold only elements. A block adds no node of its own, so its arms are
// validated as children of the enclosing element.
func (v *validator) walkBlock(blk *syntax.Block, parent string) {
	v.src = blk.Src
	// An {#each} puts its loop variable in scope for both arms, so a field
	// access inside the row is checked against the shape the list holds.
	if blk.Kind == syntax.BlockEach {
		if sh := v.checkEachSource(blk); sh != nil {
			v.scopes = append(v.scopes, map[string]*syntax.DataShape{blk.Var: sh})
			defer func() { v.scopes = v.scopes[:len(v.scopes)-1] }()
		}
	}
	for _, br := range blk.Branches {
		if br.Cond != nil {
			v.checkCond(br)
		}
		for _, m := range br.Members {
			switch x := m.(type) {
			case *syntax.Element:
				v.walk(x, parent)
			case *syntax.Block:
				v.walkBlock(x, parent)
			case *syntax.Property:
				v.addIn(x.Src, x.NameAt, x.NameAt+syntax.Pos(len(x.Name)),
					fmt.Sprintf("a block cannot declare the property %q", x.Name), "")
			}
		}
	}
}

// eachShape resolves the shape an {#each} walks, without reporting anything.
func (v *validator) eachShape(blk *syntax.Block) *syntax.DataShape {
	dv := v.file.Var(blk.Source)
	if dv == nil || !dv.List {
		return nil
	}
	return v.file.Shape(dv.Elem)
}

// checkEachSource resolves `{#each data.rows as row}`: the variable must exist,
// hold a list and name a declared shape. It returns the shape, or nil.
func (v *validator) checkEachSource(blk *syntax.Block) *syntax.DataShape {
	at := blk.SourceAt
	end := at + syntax.Pos(len(blk.Source))
	dv := v.file.Var(blk.Source)
	if dv == nil {
		v.add(at, end, fmt.Sprintf("no bridge variable %q", blk.Source), syntax.Suggest(blk.Source, varNames(v.file)))
		return nil
	}
	if !dv.List {
		v.add(at, end, fmt.Sprintf("bridge.%s is not a list: declare it as %s: [shape]", dv.Name, dv.Name), "")
		return nil
	}
	sh := v.file.Shape(dv.Elem)
	if sh == nil {
		return nil // already reported against the declaration
	}
	return sh
}

// checkCond accepts the value types that can produce a bool. A bare identifier
// or a string is a value, not a condition, and would silently never be true.
func (v *validator) checkCond(br syntax.Branch) {
	switch br.Cond.(type) {
	case *syntax.Bool, *syntax.Unary, *syntax.Binary, *syntax.Ternary, *syntax.Ref:
	case *syntax.Ident, *syntax.String, *syntax.Number, *syntax.Color, *syntax.Percent:
		v.add(br.Cond.Pos(), br.Cond.Pos()+1,
			"an {#if} condition must be a bool: a comparison, app.dark, bridge.loading or an id.prop that is a bool", "")
	}
}

func propNames(def *Element) []string {
	out := make([]string, len(def.Attrs), len(def.Attrs)+len(def.Events))
	for i, a := range def.Attrs {
		out[i] = a.Name
	}
	return append(out, def.Events...)
}

// checkValue verifies the literal kind and records the element's id. A map is
// the responsive form: one ordinary literal per breakpoint.
func (v *validator) checkValue(e *syntax.Element, p *syntax.Property, pd *Prop) {
	val := p.Value
	if m, ok := val.(*syntax.Map); ok {
		v.checkMap(e, pd, m)
		return
	}
	v.checkLiteral(e, pd, val)
}

// checkProps validates a fragment's input block: unique names, a scalar type,
// and a default that fits the declared type.
func (v *validator) checkProps() {
	v.src = ""
	seen := map[string]syntax.Pos{}
	for i := range v.file.Props {
		pr := &v.file.Props[i]
		at := pr.NameAt
		if _, dup := seen[pr.Name]; dup {
			v.add(at, at+syntax.Pos(len(pr.Name)), fmt.Sprintf("duplicate prop %q", pr.Name), "")
			continue
		}
		seen[pr.Name] = at
		ft, ok := fieldType(false, pr.Type)
		if !ok {
			v.add(pr.At, pr.At+syntax.Pos(len(pr.Type)),
				fmt.Sprintf("prop %q: %q is not a type; use bool, string, number or color", pr.Name, pr.Type),
				syntax.Suggest(pr.Type, scalarTypes))
			continue
		}
		if pr.Default != nil {
			v.checkLiteral(propsElement, &Prop{Name: pr.Name, Type: ft}, pr.Default)
		}
	}
}

// propsElement names the `props` block in diagnostics: "props.title expects …".
var propsElement = &syntax.Element{Name: "props"}

// declaredPropNames lists the props a fragment declares, for suggestions.
func declaredPropNames(f *syntax.File) []string {
	out := make([]string, 0, len(f.Props))
	for _, pr := range f.Props {
		out = append(out, pr.Name)
	}
	return out
}

// checkPropRef validates `props.name` in a fragment against the property it
// feeds, using the prop's declared type. Expansion replaces every reference
// before a design is loaded, so one that is still here was written in the
// fragment itself.
func (v *validator) checkPropRef(e *syntax.Element, pd *Prop, x *syntax.Ref, at, end syntax.Pos, free bool) {
	if v.entry {
		v.add(at, end, fmt.Sprintf("%s.%s has no value: only an imported fragment receives props", x.ID, x.Prop), "")
		return
	}
	pr := v.file.Prop(x.Prop)
	if pr == nil {
		v.add(at, end,
			fmt.Sprintf("%s.%s: no prop %q", e.Name, pd.Name, x.Prop),
			syntax.Suggest(x.Prop, declaredPropNames(v.file)))
		return
	}
	if pd.Type == TypeID {
		v.add(at, end,
			fmt.Sprintf("%s.%s cannot come from a prop: an id names a node once", e.Name, pd.Name), "")
		return
	}
	ft, ok := fieldType(false, pr.Type)
	if !ok || free {
		return
	}
	if !typesFit(ft, pd.Type) {
		v.add(at, end,
			fmt.Sprintf("%s.%s: prop %q is %s, not %s", e.Name, pd.Name, x.Prop, typeName(ft), typeName(pd.Type)), "")
	}
}

// checkMap verifies a responsive value. id is not responsive (it names a node
// once) and maps do not nest.
func (v *validator) checkMap(e *syntax.Element, pd *Prop, m *syntax.Map) {
	if pd.Type == TypeID {
		v.add(m.At, m.At+1, fmt.Sprintf("%s.%s cannot be responsive", e.Name, pd.Name), "")
		return
	}
	seen := map[string]bool{}
	for _, entry := range m.Entries {
		keyAt, keyEnd := entry.KeyAt, entry.KeyAt+syntax.Pos(len(entry.Key))
		if _, ok := BreakpointMin(entry.Key); !ok {
			v.add(keyAt, keyEnd,
				fmt.Sprintf("%s.%s has no breakpoint %q", e.Name, pd.Name, entry.Key),
				syntax.Suggest(entry.Key, BreakpointNames()))
			continue
		}
		if seen[entry.Key] {
			v.add(keyAt, keyEnd,
				fmt.Sprintf("%s.%s repeats the breakpoint %q", e.Name, pd.Name, entry.Key), "")
			continue
		}
		seen[entry.Key] = true
		if nested, ok := entry.Value.(*syntax.Map); ok {
			v.add(nested.At, nested.At+1,
				fmt.Sprintf("%s.%s: a breakpoint value cannot be another map", e.Name, pd.Name), "")
			continue
		}
		v.checkLiteral(e, pd, entry.Value)
	}
}

// checkLiteral verifies one non-map value against the property type.
func (v *validator) checkLiteral(e *syntax.Element, pd *Prop, val syntax.Value) {
	at, end := val.Pos(), val.Pos()+1
	fail := func(want string) {
		v.add(at, end, fmt.Sprintf("%s.%s expects %s, got %s", e.Name, pd.Name, want, kindName(val)), "")
	}

	if isExprValue(val) {
		// Expressions are evaluated at runtime against the property type, so a
		// string, bool, color or enum can be conditional too. A percentage is
		// still only meaningful where a parent offers a size.
		if pd.Type == TypeNumber {
			if !v.checkNumeric(e, pd, val) {
				fail("a number")
			}
			return
		}
		if valueHasPercent(val) {
			v.add(at, at+1,
				fmt.Sprintf("%s.%s: a percentage is only supported for width and height", e.Name, pd.Name), "")
		}
		return
	}

	switch pd.Type {
	case TypeID:
		txt, ok := literalText(val)
		if !ok {
			fail("an identifier")
			return
		}
		if !idPattern.MatchString(txt) {
			v.add(at, end, fmt.Sprintf("invalid id %q: must match [a-z][A-Za-z0-9_]* (or a.b, the form an instance gives its fragment's ids)", txt), "")
			return
		}
		if _, dup := v.seen[txt]; dup {
			msg := fmt.Sprintf("duplicate id %q", txt)
			if !strings.Contains(txt, ".") {
				msg += ": two fragment instances declare it; give each instance an id to scope them"
			}
			v.add(at, end, msg, "")
			return
		}
		v.seen[txt] = at
		v.ids[e] = txt
		v.elems[txt] = e

	case TypeString:
		switch val.(type) {
		case *syntax.String, *syntax.Ident:
			// An Ident is allowed so `text: hello` reads naturally; the schema
			// documents string properties as accepting bare words.
		default:
			fail("a string")
		}

	case TypeNumber:
		if !v.checkNumeric(e, pd, val) {
			fail("a number")
		}

	case TypeBool:
		if _, ok := val.(*syntax.Bool); !ok {
			fail("true or false")
		}

	case TypeColor:
		switch c := val.(type) {
		case *syntax.Color:
			// Already normalized by the lexer.
		case *syntax.String:
			if _, ok := parseColor(c.Val); !ok {
				v.add(at, end, fmt.Sprintf("%s.%s is not a valid color: %q", e.Name, pd.Name, c.Val), "")
			}
		case *syntax.Ident:
			// A bare word is a theme role such as surfaceContainerHigh.
			if _, ok := (m3.Colors{}).Role(c.Val); !ok {
				v.add(at, end,
					fmt.Sprintf("%s.%s: %q is not a color role and not a #rrggbb literal", e.Name, pd.Name, c.Val),
					syntax.Suggest(c.Val, m3.ColorRoles()))
			}
		default:
			fail("a color like #3b82f6 or a role like surfaceContainerHigh")
		}

	case TypeEnum:
		name, ok := literalText(val)
		if !ok {
			fail("an identifier (" + strings.Join(pd.Enum, ", ") + ")")
			return
		}
		for _, allowed := range pd.Enum {
			if name == allowed {
				return
			}
		}
		v.add(at, end, fmt.Sprintf("%s.%s has no value %q", e.Name, pd.Name, name), syntax.Suggest(name, pd.Enum))
	}
}

// checkNumeric accepts a number, a percentage, a reference or an arithmetic
// expression. Percentages only make sense where a parent offers a width or
// height; more than one `%` in one value would mix unrelated parent axes, so
// the whole value must be one percentage.
func (v *validator) checkNumeric(e *syntax.Element, pd *Prop, val syntax.Value) bool {
	switch x := val.(type) {
	case *syntax.Number:
		return true
	case *syntax.Percent:
		if pd.Name != "width" && pd.Name != "height" {
			v.add(x.At, x.At+1,
				fmt.Sprintf("%s.%s: a percentage is only supported for width and height", e.Name, pd.Name), "")
		}
		return true
	case *syntax.Ref:
		return true // the target is checked in walkRefs, after every id exists
	case *syntax.Binary, *syntax.Unary, *syntax.Ternary:
		if valueHasPercent(val) {
			if pd.Name != "width" && pd.Name != "height" {
				v.add(val.Pos(), val.Pos()+1,
					fmt.Sprintf("%s.%s: a percentage is only supported for width and height", e.Name, pd.Name), "")
			}
		}
		return true
	}
	return false
}

// globalProp validates the two namespaces an expression can read besides node
// properties. They are reserved ids, so a design cannot shadow them.
func globalProp(id, prop string) (bool, string) {
	var names []string
	switch id {
	case "app":
		names = []string{"theme", "mode", "systemMode", "dark", "os"}
	case "window":
		names = []string{"width", "height"}
	default:
		return true, ""
	}
	for _, n := range names {
		if n == prop {
			return true, ""
		}
	}
	return false, syntax.Suggest(prop, names)
}

func typeName(t ValueType) string { return t.String() }

// isExprValue reports whether a value is computed rather than a literal.
func isExprValue(v syntax.Value) bool {
	switch v.(type) {
	case *syntax.Ref, *syntax.Binary, *syntax.Unary, *syntax.Ternary:
		return true
	}
	return false
}

// walkRefs checks every reference once all ids are known, so a forward
// reference works like a backward one.
func (v *validator) walkRefs(e *syntax.Element) {
	def, ok := Lookup(e.Name)
	if !ok {
		return
	}
	for _, p := range e.Props() {
		if ev, ok := Event(p.Name); ok && def.HasEvent(p.Name) {
			v.checkEventCall(e, ev, p)
			continue
		}
		v.checkRefs(e, def.Prop(p.Name), p.Value)
	}
	for _, m := range e.Members {
		switch x := m.(type) {
		case *syntax.Element:
			v.walkRefs(x)
		case *syntax.Block:
			v.walkBlockRefs(x)
		}
	}
}

// boolProp stands in for the property a block condition reads, so a reference
// in `{#if id.prop}` is type-checked exactly like a bool property value.
var boolProp = &Prop{Name: "condition", Type: TypeBool}

// condElement names a block condition in diagnostics: "{#if}.condition: ...".
var condElement = &syntax.Element{Name: "{#if}"}

// anyProp marks a reference whose target type is not checked, because a
// comparison decides the type itself (`r.title == "hide"`).
var anyProp = &Prop{Name: "value", Type: TypeString}

// walkBlockRefs checks the references inside block conditions and arms.
func (v *validator) walkBlockRefs(blk *syntax.Block) {
	if blk.Kind == syntax.BlockEach {
		if sh := v.eachShape(blk); sh != nil {
			v.scopes = append(v.scopes, map[string]*syntax.DataShape{blk.Var: sh})
			defer func() { v.scopes = v.scopes[:len(v.scopes)-1] }()
		}
	}
	for _, br := range blk.Branches {
		if br.Cond != nil {
			v.checkRefs(condElement, boolProp, br.Cond)
		}
		for _, m := range br.Members {
			switch x := m.(type) {
			case *syntax.Element:
				v.walkRefs(x)
			case *syntax.Block:
				v.walkBlockRefs(x)
			}
		}
	}
}

func (v *validator) checkRefs(e *syntax.Element, pd *Prop, val syntax.Value) {
	switch x := val.(type) {
	case *syntax.Map:
		for _, entry := range x.Entries {
			v.checkRefs(e, pd, entry.Value)
		}
	case *syntax.Binary:
		// A comparison mixes two values of the same type on purpose, so
		// neither side is checked against the property; a logical operator
		// combines two bools.
		if isComparison(x.Op) {
			v.checkRefs(e, anyProp, x.Left)
			v.checkRefs(e, anyProp, x.Right)
			return
		}
		v.checkRefs(e, pd, x.Left)
		v.checkRefs(e, pd, x.Right)
	case *syntax.Unary:
		v.checkRefs(e, pd, x.X)
	case *syntax.Ternary:
		// The condition is a bool; only the two results feed the property.
		v.checkRefs(e, boolProp, x.Cond)
		v.checkRefs(e, pd, x.Then)
		v.checkRefs(e, pd, x.Else)
	case *syntax.Ref:
		if pd == nil {
			return
		}
		at := x.At
		end := at + syntax.Pos(len(x.ID)+1+len(x.Prop))
		free := pd == anyProp
		if x.ID == "props" {
			v.checkPropRef(e, pd, x, at, end, free)
			return
		}
		if x.ID == "data" || x.ID == "bridge" {
			v.checkDataRef(e, pd, x, at, end, free)
			return
		}
		if sh := v.lookupLoopVar(x.ID); sh != nil {
			v.checkFieldRef(e, pd, x, sh, at, end, free)
			return
		}
		if x.ID == "app" || x.ID == "window" {
			if ok, hint := globalProp(x.ID, x.Prop); !ok {
				v.add(at, end,
					fmt.Sprintf("%s.%s: %s has no property %q", e.Name, pd.Name, x.ID, x.Prop), hint)
			}
			return
		}
		target, ok := v.elems[x.ID]
		if !ok {
			v.add(at, end, fmt.Sprintf("%s.%s references unknown id %q", e.Name, pd.Name, x.ID), "")
			return
		}
		tdef, ok := Lookup(target.Name)
		if !ok {
			return
		}
		tp := tdef.Prop(x.Prop)
		switch {
		case tp == nil:
			v.add(at, end,
				fmt.Sprintf("%s.%s: %s has no property %q", e.Name, pd.Name, target.Name, x.Prop),
				syntax.Suggest(x.Prop, propNames(tdef)))
		case free:
			// The comparison decides the type.
		case tp.Type != pd.Type && !(tp.Type == TypeString && pd.Type == TypeEnum) && !(tp.Type == TypeEnum && pd.Type == TypeString):
			v.add(at, end,
				fmt.Sprintf("%s.%s: %s.%s is %s, not %s", e.Name, pd.Name, target.Name, x.Prop,
					typeName(tp.Type), typeName(pd.Type)), "")
		case targetValueHasPercent(target, x.Prop):
			v.add(at, end,
				fmt.Sprintf("%s.%s: %s.%s is a percentage, which cannot be referenced", e.Name, pd.Name, target.Name, x.Prop), "")
		}
	}
}

// handlerNames lists the bridge handlers, for suggestions.
func handlerNames(f *syntax.File) []string {
	out := make([]string, 0, len(f.Vars))
	for _, dv := range f.Vars {
		if dv.Fn {
			out = append(out, dv.Name)
		}
	}
	return out
}

// checkEventCall validates `onClick: bridge.onOpen(post.id)` against the handler
// the bridge declared. Arguments are values, and their types must match; the
// special identifier `event` is the event's own payload.
func (v *validator) checkEventCall(e *syntax.Element, ev EventDef, p *syntax.Property) {
	call, ok := p.Value.(*syntax.Call)
	if !ok {
		return // already reported where the property was read
	}
	at := call.Callee.At
	end := at + syntax.Pos(len(call.Callee.ID)+1+len(call.Callee.Prop))
	if call.Callee.ID != "bridge" && call.Callee.ID != "data" {
		v.add(at, end,
			fmt.Sprintf("%s.%s: a handler call starts with bridge., not %s.", e.Name, p.Name, call.Callee.ID), "")
		return
	}
	dv := v.file.Var(call.Callee.Prop)
	if dv == nil {
		v.add(at, end, fmt.Sprintf("%s.%s: no handler %q", e.Name, p.Name, call.Callee.Prop),
			syntax.Suggest(call.Callee.Prop, handlerNames(v.file)))
		return
	}
	if !dv.Fn {
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s.%s is a value, not a handler; declare it as %s: func(...)",
				e.Name, p.Name, call.Callee.ID, call.Callee.Prop, call.Callee.Prop), "")
		return
	}
	if len(call.Args) != len(dv.Args) {
		v.add(call.At, end,
			fmt.Sprintf("%s.%s: %s takes %d argument(s), got %d",
				e.Name, p.Name, call.Callee.Prop, len(dv.Args), len(call.Args)), "")
		return
	}
	for i, a := range call.Args {
		pa := dv.Args[i]
		ft, _ := fieldType(false, pa.Type)
		prop := &Prop{Name: p.Name, Type: ft}
		if id, ok := a.(*syntax.Ident); ok && id.Val == "event" {
			switch {
			case !ev.HasValue:
				v.add(a.Pos(), a.Pos()+1,
					fmt.Sprintf("%s.%s: %s carries no value, so event cannot be passed", e.Name, p.Name, p.Name), "")
			case !typesFit(ev.Type, ft):
				v.add(a.Pos(), a.Pos()+1,
					fmt.Sprintf("%s.%s: %s carries %s, which cannot feed %s",
						e.Name, p.Name, p.Name, typeName(ev.Type), typeName(ft)), "")
			}
			continue
		}
		if _, nested := a.(*syntax.Call); nested {
			v.add(a.Pos(), a.Pos()+1,
				fmt.Sprintf("%s.%s: a handler argument cannot be another call", e.Name, p.Name), "")
			continue
		}
		v.checkLiteral(e, prop, a)
		v.checkRefs(e, prop, a)
	}
}

// checkDataRef validates `data.name` against the declared variable and the
// property it feeds.
func (v *validator) checkDataRef(e *syntax.Element, pd *Prop, x *syntax.Ref, at, end syntax.Pos, free bool) {
	dv := v.file.Var(x.Prop)
	if dv == nil {
		v.add(at, end,
			fmt.Sprintf("%s.%s: no data variable %q", e.Name, pd.Name, x.Prop),
			syntax.Suggest(x.Prop, varNames(v.file)))
		return
	}
	if dv.Fn {
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s.%s is a handler; call it in an event such as onClick: bridge.%s(...)",
				e.Name, pd.Name, x.ID, x.Prop, x.Prop), "")
		return
	}
	ft, ok := fieldType(dv.List, dv.Type)
	if !ok {
		v.add(at, end, fmt.Sprintf("%s.%s: data.%s is a list; walk it with {#each}", e.Name, pd.Name, x.Prop), "")
		return
	}
	if !free && !typesFit(ft, pd.Type) {
		v.add(at, end,
			fmt.Sprintf("%s.%s: data.%s is %s, not %s", e.Name, pd.Name, x.Prop, typeName(ft), typeName(pd.Type)), "")
	}
}

// checkFieldRef validates `row.field` for a loop variable, against the shape the
// list holds.
func (v *validator) checkFieldRef(e *syntax.Element, pd *Prop, x *syntax.Ref, sh *syntax.DataShape, at, end syntax.Pos, free bool) {
	var field *syntax.DataField
	for i := range sh.Fields {
		if sh.Fields[i].Name == x.Prop {
			field = &sh.Fields[i]
			break
		}
	}
	if field == nil {
		names := make([]string, 0, len(sh.Fields))
		for _, f := range sh.Fields {
			names = append(names, f.Name)
		}
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s has no field %q", e.Name, pd.Name, sh.Name, x.Prop),
			syntax.Suggest(x.Prop, names))
		return
	}
	ft, ok := fieldType(field.List, field.Type)
	if !ok {
		v.add(at, end, fmt.Sprintf("%s.%s: %s.%s is a list; walk it with {#each}", e.Name, pd.Name, sh.Name, x.Prop), "")
		return
	}
	if !free && !typesFit(ft, pd.Type) {
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s.%s is %s, not %s", e.Name, pd.Name, sh.Name, x.Prop, typeName(ft), typeName(pd.Type)), "")
	}
}

// isComparison reports whether op compares two values.
func isComparison(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// typesFit reports whether a data field type can feed a property type. A string
// may feed an enum and the other way round, as with id references.
func typesFit(from, to ValueType) bool {
	if from == to {
		return true
	}
	if from == TypeString && to == TypeEnum || from == TypeEnum && to == TypeString {
		return true
	}
	// A string field can name a colour role, which the runtime resolves; the
	// same leniency the schema gives a bare identifier.
	if from == TypeString && to == TypeColor {
		return true
	}
	// A number can feed a string property: the runtime formats it, so
	// `text: data.count` shows the count.
	return from == TypeNumber && to == TypeString
}

// targetValueHasPercent reports whether the element declares the property as a
// percentage, directly or in a breakpoint map.
func targetValueHasPercent(e *syntax.Element, name string) bool {
	p := e.Prop(name)
	return p != nil && valueHasPercent(p.Value)
}

func valueHasPercent(val syntax.Value) bool {
	switch x := val.(type) {
	case *syntax.Percent:
		return true
	case *syntax.Binary:
		return valueHasPercent(x.Left) || valueHasPercent(x.Right)
	case *syntax.Unary:
		return valueHasPercent(x.X)
	case *syntax.Ternary:
		return valueHasPercent(x.Cond) || valueHasPercent(x.Then) || valueHasPercent(x.Else)
	case *syntax.Map:
		for _, entry := range x.Entries {
			if valueHasPercent(entry.Value) {
				return true
			}
		}
	}
	return false
}

func literalText(val syntax.Value) (string, bool) {
	switch v := val.(type) {
	case *syntax.String:
		return v.Val, true
	case *syntax.Ident:
		return v.Val, true
	}
	return "", false
}

func kindName(val syntax.Value) string {
	switch val.Kind() {
	case syntax.StringKind:
		return "a string"
	case syntax.NumberKind:
		return "a number"
	case syntax.BoolKind:
		return "a bool"
	case syntax.ColorKind:
		return "a color"
	case syntax.IdentKind:
		return "an identifier"
	case syntax.MapKind:
		return "a map"
	case syntax.PercentKind:
		return "a percentage"
	case syntax.RefKind:
		return "a reference"
	case syntax.BinaryKind, syntax.UnaryKind, syntax.TernaryKind:
		return "an expression"
	case syntax.CallKind:
		return "a handler call"
	}
	return "an unknown value"
}

// parseColor accepts #rgb, #rrggbb and #rrggbbaa.
func parseColor(s string) (uint32, bool) {
	if !strings.HasPrefix(s, "#") {
		return 0, false
	}
	hexs := s[1:]
	if len(hexs) != 3 && len(hexs) != 6 && len(hexs) != 8 {
		return 0, false
	}
	n, err := strconv.ParseUint(hexs, 16, 32)
	if err != nil {
		return 0, false
	}
	switch len(hexs) {
	case 3:
		r := (n >> 8) & 0xF
		g := (n >> 4) & 0xF
		b := n & 0xF
		return uint32(r*17)<<24 | uint32(g*17)<<16 | uint32(b*17)<<8 | 0xFF, true
	case 6:
		return uint32(n)<<8 | 0xFF, true
	default:
		return uint32(n), true
	}
}
