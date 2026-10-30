package schema

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ksckaan1/goi/internal/m3"
	"github.com/ksckaan1/goi/internal/syntax"
)

var idPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)

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
// a program opens: a Window root, and no unbound `$param`.
func validateFile(f *syntax.File, entry bool) (*Result, syntax.Diagnostics) {
	if f == nil || f.Root == nil {
		return nil, syntax.Diagnostics{{Message: "the file has no root element"}}
	}
	v := &validator{
		ids:    map[*syntax.Element]string{},
		elems:  map[string]*syntax.Element{},
		seen:   map[string]syntax.Pos{},
		entry:  entry,
		params: make(map[string]syntax.Value, len(f.Params)),
	}
	for _, pr := range f.Params {
		v.params[pr.Name] = pr.Default
	}
	if _, ok := Lookup(f.Root.Name); !ok {
		v.elementUnknown(f.Root)
	}
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
	// entry reports unbound `$param` refs, which are legal in a fragment.
	entry bool
	// params holds the declared defaults of a fragment, so a `$param` value can
	// be checked against the property it feeds.
	params map[string]syntax.Value
}

func (v *validator) add(at, end syntax.Pos, msg, hint string) {
	v.diag = append(v.diag, syntax.Diagnostic{Pos: at, End: end, Message: msg, Hint: hint, Src: v.src})
}

// addIn is add for a position that belongs to a specific file, which is the case
// when an instance overrides a fragment property.
func (v *validator) addIn(src string, at, end syntax.Pos, msg, hint string) {
	v.diag = append(v.diag, syntax.Diagnostic{Pos: at, End: end, Message: msg, Hint: hint, Src: src})
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
			v.add(at, at+syntax.Pos(len(p.Name)),
				fmt.Sprintf("unknown property %q on %s", p.Name, e.Name), syntax.Suggest(p.Name, propNames(def)))
			continue
		}
		v.checkValue(e, p, pd)
		v.src = e.Src
	}

	if len(e.Children()) > 0 && !def.Children {
		first := e.Children()[0]
		v.add(first.NameAt, first.NameAt+2, fmt.Sprintf("%s cannot contain child elements", e.Name), "")
	}

	for _, c := range e.Children() {
		v.walk(c, e.Name)
	}
}

func propNames(def *Element) []string {
	out := make([]string, len(def.Attrs))
	for i, a := range def.Attrs {
		out[i] = a.Name
	}
	return out
}

// checkValue verifies the literal kind and records the element's id. A map is
// the responsive form: one ordinary literal per breakpoint.
func (v *validator) checkValue(e *syntax.Element, p *syntax.Property, pd *Prop) {
	val := p.Value
	// Expansion replaces every `$param` before validation. One that is still here
	// was written in the fragment itself, so it is checked against the param's
	// declared default: the default decides the type the instance must pass.
	if ref, ok := val.(*syntax.ParamRef); ok {
		v.checkParamValue(e, pd, ref)
		return
	}
	if m, ok := val.(*syntax.Map); ok {
		v.checkMap(e, pd, m)
		return
	}
	v.checkLiteral(e, pd, val)
}

// checkParamValue validates a `$param` in a fragment against the property it
// feeds, using the param's default as the type witness. Anything the type check
// reports is re-pointed at the reference and prefixed with the param name, so the
// message says which value is wrong.
func (v *validator) checkParamValue(e *syntax.Element, pd *Prop, ref *syntax.ParamRef) {
	if v.entry {
		v.add(ref.At, ref.At+syntax.Pos(len(ref.Name)+1),
			fmt.Sprintf("$%s has no value: a param is only filled in when the file is imported", ref.Name), "")
		return
	}
	def, ok := v.params[ref.Name]
	if !ok {
		v.add(ref.At, ref.At+syntax.Pos(len(ref.Name)+1),
			fmt.Sprintf("%s has no param %q", e.Src, ref.Name), "")
		return
	}
	if pd.Type == TypeID {
		v.add(ref.At, ref.At+syntax.Pos(len(ref.Name)+1),
			fmt.Sprintf("%s.%s cannot come from a param: an id names a node once", e.Name, pd.Name), "")
		return
	}
	before := len(v.diag)
	v.checkLiteral(e, pd, def)
	for i := before; i < len(v.diag); i++ {
		v.diag[i].Pos = ref.At
		v.diag[i].End = ref.At + syntax.Pos(len(ref.Name)+1)
		v.diag[i].Src = v.src
		v.diag[i].Message = fmt.Sprintf("param %q: %s", ref.Name, v.diag[i].Message)
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
			v.add(at, end, fmt.Sprintf("invalid id %q: must match [a-z][A-Za-z0-9_]*", txt), "")
			return
		}
		if _, dup := v.seen[txt]; dup {
			v.add(at, end, fmt.Sprintf("duplicate id %q", txt), "")
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
		names = []string{"theme", "dark"}
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
		v.checkRefs(e, def.Prop(p.Name), p.Value)
	}
	for _, c := range e.Children() {
		v.walkRefs(c)
	}
}

func (v *validator) checkRefs(e *syntax.Element, pd *Prop, val syntax.Value) {
	switch x := val.(type) {
	case *syntax.Map:
		for _, entry := range x.Entries {
			v.checkRefs(e, pd, entry.Value)
		}
	case *syntax.Binary:
		// A comparison or ternary can mix types on purpose, so the target's
		// type is only checked for a bare reference.
		v.checkRefs(e, pd, x.Left)
		v.checkRefs(e, pd, x.Right)
	case *syntax.Unary:
		v.checkRefs(e, pd, x.X)
	case *syntax.Ternary:
		v.checkRefs(e, pd, x.Cond)
		v.checkRefs(e, pd, x.Then)
		v.checkRefs(e, pd, x.Else)
	case *syntax.Ref:
		if pd == nil {
			return
		}
		at := x.At
		end := at + syntax.Pos(len(x.ID)+1+len(x.Prop))
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
	case syntax.ParamRefKind:
		return "a param reference"
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
