package schema

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"github.com/goi-gui/goi/internal/componentdef"
	"github.com/goi-gui/goi/internal/csscolor"
	"github.com/goi-gui/goi/internal/gosetup"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/syntax"
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
	v.setupFuncs, v.setupValues = setupDecls(f)
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
	// loopTypes holds the primitive type of each loop variable that is not a
	// row: a slice's index, a map's key, or a scalar element. It is what lets
	// `bridge.onPick(i)` type-check the way Go does.
	loopTypes []map[string]*syntax.Type
	// loopDepth is non-zero inside a for body, where continue and break are
	// legal.
	loopDepth int
	// setupFuncs and setupValues are the names a file's setup blocks declare,
	// so a value can call or read them where a literal would be expected.
	setupFuncs  map[string]*syntax.Type
	setupValues map[string]*syntax.Type
}

func (v *validator) add(at, end syntax.Pos, msg, hint string) {
	v.diag = append(v.diag, syntax.Diagnostic{Pos: at, End: end, Message: msg, Hint: hint, Src: v.src})
}

// addIn is add for a position that belongs to a specific file, which is the case
// when an instance overrides a fragment property.
func (v *validator) addIn(src string, at, end syntax.Pos, msg, hint string) {
	v.diag = append(v.diag, syntax.Diagnostic{Pos: at, End: end, Message: msg, Hint: hint, Src: src})
}

// scalarTypes are the primitive names a declaration may use. They are Go's
// primitives plus `color`, which the design needs and Go spells as a uint32.
var scalarTypes = []string{
	"bool", "string", "color",
	"int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"float32", "float64", "byte", "rune",
}

// numericTypes are the declared types that become a number in a design.
var numericTypes = []string{
	"int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"float32", "float64", "byte", "rune",
}

// isNumericType reports whether a declared type is a Go number.
func isNumericType(name string) bool {
	for _, n := range numericTypes {
		if n == name {
			return true
		}
	}
	return false
}

// TypeOf maps a declared type to the value type a property checks against.
// The second result is false for a slice, a map or a handler, which are not
// values a property can read.
func TypeOf(t *syntax.Type) (ValueType, bool) {
	if t == nil || t.Kind != syntax.TypeScalar {
		return TypeString, false
	}
	switch t.Name {
	case "bool":
		return TypeBool, true
	case "string":
		return TypeString, true
	case "color":
		return TypeColor, true
	}
	if isNumericType(t.Name) {
		return TypeNumber, true
	}
	return TypeString, false
}

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
			v.checkType(f.Type, "field "+sh.Name+"."+f.Name)
		}
	}
	seenVar := map[string]syntax.Pos{}
	for _, dv := range v.file.Vars {
		if _, dup := seenVar[dv.Name]; dup {
			v.add(dv.NameAt, dv.NameAt+syntax.Pos(len(dv.Name)), fmt.Sprintf("duplicate bridge variable %q", dv.Name), "")
			continue
		}
		seenVar[dv.Name] = dv.NameAt
		if dv.Name == "data" || dv.Name == "bridge" || dv.Name == "props" || dv.Name == "app" || dv.Name == "window" || dv.Name == "formatf" || dv.Name == "t" {
			v.add(dv.NameAt, dv.NameAt+syntax.Pos(len(dv.Name)), fmt.Sprintf("%q is a reserved name", dv.Name), "")
		}
		v.checkType(dv.Type, "bridge "+dv.Name)
	}
}

// checkType validates one declared type: a primitive, a slice or map of one, a
// declared shape (or a slice of one), or a func whose parameters are primitives.
func (v *validator) checkType(t syntax.Type, what string) {
	v.src = ""
	switch t.Kind {
	case syntax.TypeSlice:
		if t.Elem == nil {
			return
		}
		if t.Elem.Kind == syntax.TypeScalar && v.file.Shape(t.Elem.Name) == nil && !isPrimitive(t.Elem.Name) {
			v.add(t.Elem.At, t.Elem.At+syntax.Pos(len(t.Elem.Name)),
				fmt.Sprintf("%s: unknown type %q", what, t.Elem.Name),
				syntax.Suggest(t.Elem.Name, append(scalarTypes, shapeNames(v.file)...)))
			return
		}
		v.checkType(*t.Elem, what)
	case syntax.TypeMap:
		if t.Name != "string" {
			v.add(t.At, t.At+syntax.Pos(len("map")),
				fmt.Sprintf("%s: a map key must be a string, not %q", what, t.Name), "")
		}
		if t.Elem != nil {
			v.checkType(*t.Elem, what)
		}
	case syntax.TypeFunc:
		for _, a := range t.Args {
			v.checkType(a.Type, what)
			if _, ok := TypeOf(&a.Type); !ok {
				v.add(a.At, a.At+syntax.Pos(len(a.Type.String())),
					fmt.Sprintf("handler parameter %q must be a primitive", a.Name), "")
			}
		}
		if t.Result != nil {
			v.checkType(*t.Result, what+" result")
		}
	default:
		if isPrimitive(t.Name) || v.file.Shape(t.Name) != nil {
			return
		}
		v.add(t.At, t.At+syntax.Pos(len(t.Name)), fmt.Sprintf("%s: unknown type %q", what, t.Name),
			syntax.Suggest(t.Name, append(scalarTypes, shapeNames(v.file)...)))
	}
}

// isPrimitive reports whether name is one of the primitive type names.
func isPrimitive(name string) bool {
	for _, n := range scalarTypes {
		if n == name {
			return true
		}
	}
	return false
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

// lookupLoopType returns the primitive type a loop variable holds, if it is a
// scalar variable in scope.
func (v *validator) lookupLoopType(name string) *syntax.Type {
	for i := len(v.loopTypes) - 1; i >= 0; i-- {
		if t, ok := v.loopTypes[i][name]; ok {
			return t
		}
	}
	return nil
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

func (v *validator) elementUnknown(e *syntax.Element) {
	v.add(e.NameAt, e.NameAt+2, fmt.Sprintf("unknown element %q", e.Name), syntax.Suggest(e.Name, ElementNames()))
}

// walk validates one element and recurses into its children. parent is the
// enclosing element name, "" for the root.
func (v *validator) walk(e *syntax.Element, parent string) {
	v.src = e.Src
	if strings.HasPrefix(e.Name, "$") {
		// A slot belongs to the component that declares it: a direct child or
		// one inside a control block under it.
		if componentdef.Slot(parent, e.Name) {
			v.walkSlot(e, parent)
			return
		}
		owner := componentdef.SlotOwner(e.Name)
		msg := fmt.Sprintf("%s is a slot: write it directly inside the component that declares it", e.Name)
		if owner != "" {
			msg = fmt.Sprintf("%s is a slot of %s: write it directly inside one", e.Name, owner)
		}
		v.add(e.NameAt, e.NameAt+syntax.Pos(len(e.Name)), msg, "")
		return
	}
	def, ok := Lookup(e.Name)
	if !ok {
		v.elementUnknown(e)
		return // children of an unknown element cannot be meaningfully checked
	}
	if e.Name == "Positioned" && parent != "Stack" {
		v.add(e.NameAt, e.NameAt+syntax.Pos(len(e.Name)), "Positioned must be a direct child of Stack", "")
	}
	if parent == "RichText" && e.Name != "Text" {
		v.add(e.NameAt, e.NameAt+syntax.Pos(len(e.Name)), "RichText children are its runs, so each one is a Text", "")
	}
	// A chart's children are its series, and only its children: a ChartSeries
	// anywhere else is data with nothing to belong to.
	if e.Name == "ChartSeries" && !isChart(parent) {
		v.add(e.NameAt, e.NameAt+syntax.Pos(len(e.Name)), "ChartSeries must be a direct child of a chart", "")
	}
	if isChart(e.Name) {
		for _, m := range e.Members {
			if x, ok := m.(*syntax.Element); ok && x.Name != "ChartSeries" {
				v.add(x.NameAt, x.NameAt+syntax.Pos(len(x.Name)),
					fmt.Sprintf("%s children are its series, so each one is a ChartSeries", e.Name), "")
			}
		}
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

// walkSlot checks a `$name` child slot: it takes elements only, and its content
// is ordinary children of the component that declares it.
func (v *validator) walkSlot(e *syntax.Element, owner string) {
	v.src = e.Src
	if e.Open == e.NameAt {
		// A built-in declares its slot itself, so the design is the caller: it
		// fills the slot with braces, the same shape a fragment call uses.
		v.add(e.NameAt, e.NameAt+syntax.Pos(len(e.Name)),
			fmt.Sprintf("%s of %s takes its elements here: write %s { … }", e.Name, owner, e.Name), "")
	}
	if props := e.Props(); len(props) > 0 {
		p := props[0]
		v.add(p.NameAt, p.NameAt+syntax.Pos(len(p.Name)),
			fmt.Sprintf("%s takes elements only: a slot is filled with children, not properties", e.Name), "")
	}
	for _, m := range e.Members {
		switch x := m.(type) {
		case *syntax.Element:
			if componentdef.Slot(e.Name, x.Name) {
				v.walkSlot(x, e.Name)
				continue
			}
			v.walk(x, owner)
		case *syntax.Block:
			v.walkBlock(x, owner)
		}
	}
}

// childStart is where a child member begins, for a diagnostic range.
type childStart struct {
	At   syntax.Pos
	Name string
}

// firstChild returns the first child element or block of e, or nil. A slot is
// not content: a built-in `$name` child is how a component takes a body, so it
// does not make a component that takes no children into one that does.
func firstChild(e *syntax.Element) *childStart {
	for _, m := range e.Members {
		switch x := m.(type) {
		case *syntax.Element:
			if len(x.Name) > 0 && x.Name[0] == '$' {
				continue
			}
			return &childStart{At: x.NameAt, Name: x.Name}
		case *syntax.Block:
			return &childStart{At: x.At, Name: "{#if}"}
		}
	}
	return nil
}

// walkBlock checks one control block: an if arm's condition is a bool, and a
// for block's range value is a list, a map or a number, with its loop variables
// bound for the body.
func (v *validator) walkBlock(blk *syntax.Block, parent string) {
	v.src = blk.Src
	if blk.Kind == syntax.BlockContinue || blk.Kind == syntax.BlockBreak {
		if v.loopDepth == 0 {
			name := "continue"
			if blk.Kind == syntax.BlockBreak {
				name = "break"
			}
			v.add(blk.At, blk.At+syntax.Pos(len(name)), name+" is only allowed inside a for loop", "")
		}
		return
	}
	if blk.Kind == syntax.BlockFor {
		v.walkFor(blk, parent)
		return
	}
	if blk.Kind == syntax.BlockSwitch {
		v.checkSwitch(blk)
	}
	for _, br := range blk.Branches {
		if br.Cond != nil {
			v.checkCond(br)
		}
		v.walkArm(br.Members, parent)
	}
}

// checkSwitch rejects what Go rejects about a switch's arms: at most one
// default. The subject and the case values are ordinary expressions, so their
// types are left to the comparison at runtime.
func (v *validator) checkSwitch(blk *syntax.Block) {
	seenDefault := false
	for _, br := range blk.Branches {
		if br.Vals != nil {
			continue
		}
		if seenDefault {
			v.add(br.At, br.At+1, "a switch has at most one default", "")
			continue
		}
		seenDefault = true
	}
}

// walkArm validates the elements of one arm.
func (v *validator) walkArm(members []syntax.Member, parent string) {
	for _, m := range members {
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

// walkFor validates a range loop and binds its loop variables.
func (v *validator) walkFor(blk *syntax.Block, parent string) {
	keyShape, valShape, keyType, valType := v.checkForRange(blk)
	defer func() {
		v.scopes = v.scopes[:len(v.scopes)-1]
		v.loopTypes = v.loopTypes[:len(v.loopTypes)-1]
	}()
	v.scopes = append(v.scopes, map[string]*syntax.DataShape{})
	v.loopTypes = append(v.loopTypes, map[string]*syntax.Type{})
	top := v.scopes[len(v.scopes)-1]
	types := v.loopTypes[len(v.loopTypes)-1]
	for _, lv := range []struct {
		name string
		at   syntax.Pos
	}{{blk.LoopKey(), blk.KeyAt}, {blk.LoopVal(), blk.ValAt}} {
		if reservedValueName(lv.name) {
			v.add(lv.at, lv.at+syntax.Pos(len(lv.name)),
				fmt.Sprintf("%q is a reserved name; a loop variable cannot shadow it", lv.name), "")
		}
	}
	if k := blk.LoopKey(); k != "" {
		if keyShape != nil {
			top[k] = keyShape
		} else if keyType != nil {
			types[k] = keyType
		}
	}
	if k := blk.LoopVal(); k != "" {
		if valShape != nil {
			top[k] = valShape
		} else if valType != nil {
			types[k] = valType
		}
	}
	v.loopDepth++
	for _, br := range blk.Branches {
		v.walkArm(br.Members, parent)
	}
	v.loopDepth--
}

// intType is the type of a slice index, which is Go's int.
func intType() *syntax.Type {
	return &syntax.Type{Kind: syntax.TypeScalar, Name: "int"}
}

// keyTypeOf is the type of the first loop variable for a ranged value: the key's
// own type for a map, an int for a slice, Go's rule.
func keyTypeOf(t *syntax.Type) *syntax.Type {
	if t != nil && t.Kind == syntax.TypeMap {
		return &syntax.Type{Kind: syntax.TypeScalar, Name: t.Name}
	}
	return intType()
}

// valueTypeOf is the type of the second loop variable: a scalar, a nested
// slice, or the value of a map. A row is a shape, not a type.
func valueTypeOf(t *syntax.Type, f *syntax.File) *syntax.Type {
	if t == nil || t.Elem == nil {
		return nil
	}
	if t.Elem.Kind == syntax.TypeScalar && f.Shape(t.Elem.Name) != nil {
		return nil // a row: its fields are what a design reads
	}
	return t.Elem
}

// reservedValueName reports a name a reference reads as a namespace, so a loop
// variable cannot take it.
func reservedValueName(name string) bool {
	switch name {
	case "app", "window", "theme", "customColor", "bridge", "data", "props":
		return true
	}
	return false
}

// checkForRange resolves `range X` and returns what the loop variables hold: a
// shape for a row, a type for a scalar, and an int key for a slice or the key's
// type for a map, Go's rule. A number range gives an int key and no value.
func (v *validator) checkForRange(blk *syntax.Block) (keyShape, valShape *syntax.DataShape, keyType, valType *syntax.Type) {
	at, end := blk.RangeAt, blk.RangeAt+1
	if blk.Range != nil {
		end = blk.RangeAt + syntax.Pos(len(edgeOf(blk.Range)))
	}
	ref, ok := blk.Range.(*syntax.Ref)
	if !ok {
		switch x := blk.Range.(type) {
		case *syntax.Ident:
			if t, ok := v.setupValues[x.Val]; ok {
				if t == nil {
					return nil, nil, intType(), nil // a setup value whose type is not known here
				}
				switch t.Kind {
				case syntax.TypeSlice, syntax.TypeMap:
					return nil, shapeFor(t.Elem, v.file), keyTypeOf(t), valueTypeOf(t, v.file)
				case syntax.TypeScalar:
					switch t.Name {
					case "int", "int8", "int16", "int32", "int64",
						"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune":
						return nil, nil, intType(), nil
					}
				}
				v.add(at, end, fmt.Sprintf("cannot range over %s: it is %s, not a slice, a map or a number",
					x.Val, t.String()), "")
				return nil, nil, nil, nil
			}
			if v.lookupLoopType(x.Val) != nil || v.lookupLoopVar(x.Val) != nil {
				return nil, nil, intType(), nil // `range imageList`
			}
		case *syntax.Number, *syntax.Binary:
			return nil, nil, intType(), nil // `for i := range 5`
		case *syntax.Call:
			if x.Name == "len" || x.Name == "cap" {
				return nil, nil, intType(), nil // `for i := range len(bridge.rows)`
			}
		}
		v.add(at, end, "a for loop ranges over a list, a map or a number", "")
		return nil, nil, nil, nil
	}
	var typ *syntax.Type
	switch ref.ID {
	case "bridge", "data":
		dv := v.file.Var(ref.Prop)
		if dv == nil {
			v.add(at, end, fmt.Sprintf("no bridge variable %q", ref.Prop),
				syntax.Suggest(ref.Prop, varNames(v.file)))
			return nil, nil, nil, nil
		}
		typ = &dv.Type
	case "props":
		pr := v.file.Prop(ref.Prop)
		if pr == nil {
			v.add(at, end, fmt.Sprintf("no prop %q", ref.Prop),
				syntax.Suggest(ref.Prop, declaredPropNames(v.file)))
			return nil, nil, nil, nil
		}
		typ = &pr.Type
	default:
		if sh := v.lookupLoopVar(ref.ID); sh != nil {
			for i := range sh.Fields {
				if sh.Fields[i].Name != ref.Prop {
					continue
				}
				typ = &sh.Fields[i].Type
				break
			}
		}
	}
	if typ == nil {
		v.add(at, end, fmt.Sprintf("cannot range over %s", ref.Text()), "")
		return nil, nil, nil, nil
	}
	if len(ref.Path) > 0 {
		field, sh, missing := resolveFieldPath(v.file, typ, ref.Path)
		if field == nil {
			v.add(at, end, pathDiagnostic(ref, typ, sh, missing), pathSuggestion(sh, missing))
			return nil, nil, nil, nil
		}
		typ = &field.Type
	}
	if typ.Kind == syntax.TypeScalar && isNumericType(typ.Name) {
		return nil, nil, intType(), nil // `for i := range bridge.count`
	}
	if typ.Kind != syntax.TypeSlice && typ.Kind != syntax.TypeMap {
		v.add(at, end, fmt.Sprintf("cannot range over %s: it is %s, not a slice, a map or a number",
			ref.Text(), typ.String()), "")
		return nil, nil, nil, nil
	}
	return nil, shapeFor(typ.Elem, v.file), keyTypeOf(typ), valueTypeOf(typ, v.file)
}

// shapeFor resolves a type to the declared shape it names, or nil.
func shapeFor(t *syntax.Type, f *syntax.File) *syntax.DataShape {
	if t == nil || t.Kind != syntax.TypeScalar {
		return nil
	}
	return f.Shape(t.Name)
}

// resolveFieldPath walks the fields a reference names after its first one,
// returning the field it ends on. A nil field also returns the shape and the
// name it did not have, so the caller can suggest the real ones.
func resolveFieldPath(f *syntax.File, t *syntax.Type, path []string) (field *syntax.DataField, sh *syntax.DataShape, missing string) {
	for _, name := range path {
		sh = shapeFor(t, f)
		if sh == nil {
			return nil, nil, name
		}
		field = nil
		for i := range sh.Fields {
			if sh.Fields[i].Name == name {
				field = &sh.Fields[i]
				break
			}
		}
		if field == nil {
			return nil, sh, name
		}
		t = &field.Type
	}
	return field, nil, ""
}

// fieldNames lists a shape's fields, for a "did you mean" hint.
func fieldNames(sh *syntax.DataShape) []string {
	if sh == nil {
		return nil
	}
	out := make([]string, len(sh.Fields))
	for i := range sh.Fields {
		out[i] = sh.Fields[i].Name
	}
	return out
}

// pathDiagnostic explains the first field of a nested reference that did not
// resolve.
func pathDiagnostic(ref *syntax.Ref, t *syntax.Type, sh *syntax.DataShape, missing string) string {
	if sh == nil {
		return fmt.Sprintf("%s has no field %q: it is %s, not a declared type", ref.Text(), missing, t.String())
	}
	return fmt.Sprintf("%s has no field %q", ref.Text(), missing)
}

func pathSuggestion(sh *syntax.DataShape, missing string) string {
	return syntax.Suggest(missing, fieldNames(sh))
}

// edgeOf returns a short text for a value, so a diagnostic can cover it.
func edgeOf(val syntax.Value) string {
	return fmt.Sprintf("%v", val.Kind())
}

// checkCond accepts the value types that can produce a bool. A bare identifier
// or a string is a value, not a condition, and would silently never be true.
func (v *validator) checkCond(br syntax.Branch) {
	switch x := br.Cond.(type) {
	case *syntax.Bool, *syntax.Unary, *syntax.Binary, *syntax.Ternary, *syntax.Ref:
	case *syntax.Ident:
		// A setup variable is typed by the interpreter; a known non-bool is
		// never true, so reject it here. An unknown initializer is left to Go.
		if t, ok := v.setupValues[x.Val]; ok {
			if t == nil || (t.Kind == syntax.TypeScalar && t.Name == "bool") {
				return
			}
			v.add(br.Cond.Pos(), br.Cond.Pos()+1,
				fmt.Sprintf("an if condition must be a bool: %s is %s", x.Val, t.String()), "")
			return
		}
		v.add(br.Cond.Pos(), br.Cond.Pos()+1,
			"an if condition must be a bool: a comparison, app.dark, bridge.loading or an id.prop that is a bool", "")
	case *syntax.String, *syntax.Number, *syntax.Color, *syntax.Percent:
		v.add(br.Cond.Pos(), br.Cond.Pos()+1,
			"an if condition must be a bool: a comparison, app.dark, bridge.loading or an id.prop that is a bool", "")
	}
}

func propNames(def *Element) []string {
	out := make([]string, len(def.Attrs), len(def.Attrs)+len(def.Events))
	for i, a := range def.Attrs {
		out[i] = a.Name
	}
	return append(out, def.Events...)
}

// checkValue verifies the literal kind and records the element's id.
func (v *validator) checkValue(e *syntax.Element, p *syntax.Property, pd *Prop) {
	v.checkEntryValue(e, pd, p.Value)
}

// checkEntryValue checks one value inside a map or modifier entry, where a
// nested modifier is legal.
func (v *validator) checkEntryValue(e *syntax.Element, pd *Prop, val syntax.Value) {
	// A setup variable is a value whose Go type is checked by the interpreter;
	// accepting its name here keeps the schema from calling a number a string.
	if id, ok := val.(*syntax.Ident); ok {
		if _, exists := v.setupValues[id.Val]; exists {
			return
		}
	}
	switch x := val.(type) {
	case *syntax.Map:
		v.checkMap(e, pd, x)
	case *syntax.Modifier:
		v.checkModifier(e, pd, x)
	default:
		v.checkLiteral(e, pd, val)
	}
}

// checkProps validates a fragment's input block: unique names and a declared
// type. A prop is filled by the instance, so it has no default.
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
		if pr.Name == "props" {
			v.add(at, at+syntax.Pos(len(pr.Name)), `"props" is a reserved name`, "")
		}
		v.checkType(pr.Type, "prop "+pr.Name)
	}
}

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
	ft := &pr.Type
	if len(x.Path) > 0 {
		field, sh, missing := resolveFieldPath(v.file, ft, x.Path)
		if field == nil {
			v.add(at, end, pathDiagnostic(x, ft, sh, missing), pathSuggestion(sh, missing))
			return
		}
		ft = &field.Type
	}
	nested := len(x.Path) > 0
	switch ft.Kind {
	case syntax.TypeFunc:
		if nested {
			v.add(at, end,
				fmt.Sprintf("%s.%s: %s is a handler; call it in an event", e.Name, pd.Name, x.Text()), "")
		} else {
			v.add(at, end,
				fmt.Sprintf("%s.%s: props.%s is a handler; call it in an event, as onClick: props.%s(...)", e.Name, pd.Name, pr.Name, pr.Name), "")
		}
		return
	case syntax.TypeSlice, syntax.TypeMap:
		if pd == lenProp {
			return // len and cap take a list
		}
		if pd.Type == TypeList && listSliceFits(*ft, pd.Elem) {
			return
		}
		if nested {
			v.add(at, end,
				fmt.Sprintf("%s.%s: %s is not a value; walk it with for i, row := range %s",
					e.Name, pd.Name, x.Text(), x.Text()), "")
		} else {
			v.add(at, end,
				fmt.Sprintf("%s.%s: props.%s is not a value; walk it with for i, row := range props.%s", e.Name, pd.Name, pr.Name, pr.Name), "")
		}
		return
	}
	vft, ok := TypeOf(ft)
	if !ok || free {
		return
	}
	if !typesFit(vft, pd.Type) {
		if nested {
			v.add(at, end,
				fmt.Sprintf("%s.%s: %s is %s, not %s", e.Name, pd.Name, x.Text(), typeName(vft), typeName(pd.Type)), "")
		} else {
			v.add(at, end,
				fmt.Sprintf("%s.%s: prop %q is %s, not %s", e.Name, pd.Name, x.Prop, typeName(vft), typeName(pd.Type)), "")
		}
	}
}

// checkMap verifies the older brace map, which is the responsive form.
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
		v.checkEntryValue(e, pd, entry.Value)
	}
}

// checkModifier verifies a responsive, conditional or state value block. Every
// entry value may itself be a modifier, so the three forms nest in any order.
func (v *validator) checkModifier(e *syntax.Element, pd *Prop, m *syntax.Modifier) {
	if pd.Type == TypeID {
		v.add(m.At, m.At+1, fmt.Sprintf("%s.%s cannot be %s", e.Name, pd.Name, m.Form), "")
		return
	}
	if len(m.Entries) == 0 {
		v.add(m.At, m.At+1, fmt.Sprintf("%s.%s: an empty %s block does nothing", e.Name, pd.Name, m.Form), "")
		return
	}
	switch m.Form {
	case syntax.ModifierResponsive:
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
			v.checkEntryValue(e, pd, entry.Value)
		}
	case syntax.ModifierState:
		seen := map[string]bool{}
		for _, entry := range m.Entries {
			keyAt, keyEnd := entry.KeyAt, entry.KeyAt+syntax.Pos(len(entry.Key))
			if !StateName(entry.Key) {
				v.add(keyAt, keyEnd,
					fmt.Sprintf("%s.%s has no state %q", e.Name, pd.Name, entry.Key),
					syntax.Suggest(entry.Key, StateNames))
				continue
			}
			if seen[entry.Key] {
				v.add(keyAt, keyEnd,
					fmt.Sprintf("%s.%s repeats the state %q", e.Name, pd.Name, entry.Key), "")
				continue
			}
			seen[entry.Key] = true
			v.checkEntryValue(e, pd, entry.Value)
		}
	case syntax.ModifierConditional:
		sawDefault := false
		for _, entry := range m.Entries {
			if entry.Cond == nil {
				if sawDefault {
					v.add(entry.At, entry.At+1,
						fmt.Sprintf("%s.%s repeats the default value", e.Name, pd.Name), "")
				}
				sawDefault = true
				v.checkEntryValue(e, pd, entry.Value)
				continue
			}
			if !isExprValue(entry.Cond) {
				if b, ok := entry.Cond.(*syntax.Bool); !ok || !b.Val {
					v.add(entry.Cond.Pos(), entry.Cond.Pos()+1,
						fmt.Sprintf("%s.%s: a condition must be a bool, got %s", e.Name, pd.Name, kindName(entry.Cond)), "")
				}
			}
			v.checkEntryValue(e, pd, entry.Value)
		}
	}
}

// checkLiteral verifies one non-map value against the property type.
func (v *validator) checkLiteral(e *syntax.Element, pd *Prop, val syntax.Value) {
	at, end := val.Pos(), val.Pos()+1
	fail := func(want string) {
		v.add(at, end, fmt.Sprintf("%s.%s expects %s, got %s", e.Name, pd.Name, want, kindName(val)), "")
	}

	// A bare name may be a loop variable, and Go says what it holds: an index
	// for a slice, a key for a map, the element for a scalar list. It is a value
	// of that type, not the literal word a string property would read.
	if id, ok := val.(*syntax.Ident); ok {
		if t := v.lookupLoopType(id.Val); t != nil {
			if ft, ok := TypeOf(t); ok && !typesFit(ft, pd.Type) {
				v.add(at, end, fmt.Sprintf("%s.%s: %s is %s, which cannot feed %s",
					e.Name, pd.Name, id.Val, typeName(ft), typeName(pd.Type)), "")
			}
			return
		}
	}

	if isExprValue(val) {
		// `theme.primary` and `customColor.brand` are colour references, not
		// expressions: the checker knows both namespaces.
		if r, ok := val.(*syntax.Ref); ok && pd.Type == TypeColor {
			switch r.ID {
			case "theme":
				if _, ok := (m3.Colors{}).Role(r.Prop); !ok {
					v.add(at, end,
						fmt.Sprintf("%s.%s: %q is not a theme colour", e.Name, pd.Name, r.Prop),
						syntax.Suggest(r.Prop, m3.ColorRoles()))
				}
				return
			case "customColor":
				if !idPattern.MatchString(r.Prop) {
					v.add(at, end, fmt.Sprintf("%s.%s: %q is not a custom colour name", e.Name, pd.Name, r.Prop), "")
				}
				return
			}
			// Any other reference is an ordinary expression (an id's property,
			// props.x, a bridge field) and goes through the expression path.
		}
		// Expressions are evaluated at runtime against the property type, so a
		// string, bool, color or enum can be conditional too. A percentage is
		// still only meaningful where a parent offers a size.
		if pd.Type == TypeNumber {
			if !v.checkNumeric(e, pd, val) {
				fail("a number")
			}
			return
		}
		if valueHasPercent(val) && !percentAllowed(pd.Name) {
			v.add(at, at+1,
				fmt.Sprintf("%s.%s: a percentage is only supported for width, height and opacity", e.Name, pd.Name), "")
		}
		return
	}

	switch pd.Type {
	case TypeTransition:
		if _, ok := val.(*syntax.Transition); !ok {
			fail("a transition list like `width 500ms, background 1s`")
		}
		return
	case TypeDuration:
		if _, ok := val.(*syntax.Duration); !ok {
			fail("a duration like `500ms`, `1s` or `0`")
		}
		return
	case TypeThemeTransition:
		if _, ok := val.(*syntax.ThemeTransition); !ok {
			fail("a theme transition like `crossfade 500ms`, `wipe 500ms` or `none`")
		}
		return
	case TypeBackdropBlur:
		switch x := val.(type) {
		case *syntax.Number:
			if x.Val < 0 {
				fail("a non-negative blur radius or a blur gradient")
			}
			return
		case *syntax.String:
			if !csscolor.IsGradient(x.Val) {
				fail("a blur radius or a linear-gradient/radial-gradient of radii")
				return
			}
			if _, ok := csscolor.ParseValueGradient(x.Val); !ok {
				fail("a valid blur gradient such as `linear-gradient(90deg, 0 0%, 6 60%, 0 100%)`")
			}
			return
		case *syntax.Call, *syntax.Binary, *syntax.Unary, *syntax.Ternary, *syntax.Ref:
			// A numeric expression evaluates at runtime and lands in the
			// scalar form; a gradient expression is not supported.
			if !v.checkNumeric(e, pd, val) {
				fail("a number or a blur gradient")
			}
			return
		}
		fail("a blur radius or a linear-gradient/radial-gradient of radii")
		return
	case TypeRotate:
		if _, ok := val.(*syntax.Rotate); !ok {
			fail("a rotate value: none, 90deg, x 90deg or 1 1 1 90deg")
		}
		return
	case TypeScale:
		if _, ok := val.(*syntax.Scale); !ok {
			fail("a scale value: none, 2, 50%, 2 0.5 or 200% 50% 200%")
		}
		return
	case TypeList:
		list, ok := val.(*syntax.List)
		if !ok {
			fail(fmt.Sprintf("a list like %s", listExample(pd.Elem)))
			return
		}
		for _, item := range list.Items {
			// An expression is only typed when it runs, so the checker leaves
			// it alone. A colour role is known now, which is why the one thing
			// it can still get wrong is asking a number list for a colour.
			if isColorRefValue(item) && pd.Elem != TypeColor {
				at := item.Pos()
				v.add(at, at+syntax.Pos(listItemLen(item)),
					fmt.Sprintf("%s: a colour role does not belong in a %s list", pd.Name, pd.Elem), "")
				continue
			}
			if dynamicValue(item) {
				continue
			}
			if _, err := ListItem(pd.Elem, item); err != nil {
				at := item.Pos()
				v.add(at, at+syntax.Pos(listItemLen(item)), fmt.Sprintf("%s: %v", pd.Name, err), "")
			}
		}
		return
	case TypeScalar:
		switch val.(type) {
		case *syntax.String, *syntax.Ident, *syntax.Number, *syntax.Bool:
			return
		}
		fail("a string, number or bool")
		return
	case TypeShadow:
		sh, ok := val.(*syntax.Shadow)
		if !ok {
			fail("a shadow, written `x y [blur [spread]] [color] [inset]`")
			return
		}
		v.checkShadow(e, pd, sh)
		return
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
		case *syntax.String, *syntax.Ident, *syntax.Call:
			// An Ident is allowed so `text: hello` reads naturally; the schema
			// documents string properties as accepting bare words.
			if pd.Name == "decoration" {
				if txt, ok := literalText(val); ok {
					v.checkDecoration(e, pd, txt, at, end)
				}
			}
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
			if csscolor.IsGradient(c.Val) {
				if pd.Name != "background" {
					v.add(at, end, fmt.Sprintf("%s.%s cannot be a gradient: a gradient is a box fill, so only background takes one", e.Name, pd.Name), "")
					return
				}
				if _, ok := csscolor.ParseGradient(c.Val); !ok {
					v.add(at, end, fmt.Sprintf("%s.%s is not a valid gradient: %q", e.Name, pd.Name, c.Val), "")
				}
				return
			}
			if _, ok := csscolor.ParseWithOpacity(c.Val); !ok {
				v.add(at, end, fmt.Sprintf("%s.%s is not a valid color: %q (a hex literal, a CSS colour name, rgb()/rgba() or a gradient)", e.Name, pd.Name, c.Val), "")
			}
		case *syntax.Ident:
			// A CSS keyword is a fixed colour. A scheme role has to be written
			// as a reference: a bare `surfaceContainerHigh` is a typo the
			// compiler can catch, and a bare word that is neither a role nor a
			// CSS colour is not a colour at all.
			if _, ok := csscolor.ParseWithOpacity(c.Val); ok {
				return
			}
			if _, ok := (m3.Colors{}).Role(c.Val); ok {
				v.add(at, end, fmt.Sprintf("%s.%s: %q is a theme colour; write theme.%s",
					e.Name, pd.Name, c.Val, c.Val), "")
				return
			}
			v.add(at, end, fmt.Sprintf("%s.%s: %q is not a colour; write theme.<role> or customColor.<name>, or a CSS colour such as #rrggbb",
				e.Name, pd.Name, c.Val), "")
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
		if !percentAllowed(pd.Name) {
			v.add(x.At, x.At+1,
				fmt.Sprintf("%s.%s: a percentage is only supported for width, height and opacity", e.Name, pd.Name), "")
		}
		return true
	case *syntax.Ref:
		return true // the target is checked in walkRefs, after every id exists
	case *syntax.Call:
		// A setup function is typed by its Go result; formatf and t build a
		// string, so they are not numbers here. An unknown result is left to
		// the interpreter rather than reported as the wrong kind.
		if t, ok := v.setupFuncs[x.Name]; ok {
			if t == nil {
				return true
			}
			ft, known := TypeOf(t)
			return !known || typesFit(ft, TypeNumber)
		}
		switch x.Name {
		case "len", "cap", "min", "max":
			return true
		}
		return false
	case *syntax.Binary, *syntax.Unary, *syntax.Ternary:
		if valueHasPercent(val) && !percentAllowed(pd.Name) {
			v.add(val.Pos(), val.Pos()+1,
				fmt.Sprintf("%s.%s: a percentage is only supported for width, height and opacity", e.Name, pd.Name), "")
		}
		return true
	}
	return false
}

// percentAllowed reports whether a property resolves a percentage. Size uses
// the parent's space; opacity uses the whole range, so 50% is 0.5.
func percentAllowed(name string) bool {
	return name == "width" || name == "height" || name == "opacity"
}

// checkDecoration checks the keywords a `decoration` value combines. `none`
// stands alone; the other three may appear together.
func (v *validator) checkDecoration(e *syntax.Element, pd *Prop, text string, at, end syntax.Pos) {
	words := strings.Fields(text)
	for _, w := range words {
		switch w {
		case "underline", "overline", "line-through":
		case "none":
			if len(words) > 1 {
				v.add(at, end, fmt.Sprintf("%s.%s: `none` cannot be combined with another decoration", e.Name, pd.Name), "")
				return
			}
		default:
			v.add(at, end, fmt.Sprintf("%s.%s has no decoration %q", e.Name, pd.Name, w),
				syntax.Suggest(w, []string{"none", "underline", "overline", "line-through"}))
			return
		}
	}
}

// checkShadow checks a shadow list: offsets, a non-negative blur and spread, and
// a colour that is a hex literal or a name. A shadow is CSS-shaped, so the
// validator's job is the shape, not the numbers: a negative offset is how every
// text shadow works.
func (v *validator) checkShadow(e *syntax.Element, pd *Prop, sh *syntax.Shadow) {
	for _, l := range sh.Layers {
		if l.Blur < 0 || l.Spread < 0 {
			v.add(l.At, l.At+1,
				fmt.Sprintf("%s.%s: blur and spread cannot be negative", e.Name, pd.Name), "")
		}
		switch c := l.Color.(type) {
		case nil, *syntax.Color:
		case *syntax.Ref:
			// `theme.primary` or `customColor.brand`: the namespace is what
			// keeps the scheme and the app's own names apart. An unknown name is
			// invisible here and warns at runtime, like a colour property.
			if c.ID != "theme" && c.ID != "customColor" || c.Prop == "" {
				v.add(c.At, c.At+syntax.Pos(len(c.ID)+len(c.Prop)+1),
					fmt.Sprintf("%s.%s: a shadow color names theme.<role> or customColor.<name>", e.Name, pd.Name), "")
			}
		case *syntax.String:
			if _, ok := csscolor.ParseWithOpacity(c.Val); !ok {
				v.add(c.At, c.At+syntax.Pos(len(c.Val)+2),
					fmt.Sprintf("%s.%s is not a valid shadow color: %q", e.Name, pd.Name, c.Val), "")
			}
		case *syntax.Ident:
			v.add(c.At, c.At+syntax.Pos(len(c.Val)),
				fmt.Sprintf("%s.%s: a shadow color needs its namespace: theme.%s or customColor.<name>",
					e.Name, pd.Name, c.Val), "")
		default:
			v.add(l.At, l.At+1,
				fmt.Sprintf("%s.%s: a shadow color is a hex literal, a CSS name, or theme.<role>/customColor.<name>", e.Name, pd.Name), "")
		}
	}
}

// globalProp validates the two namespaces an expression can read besides node
// properties. They are reserved ids, so a design cannot shadow them.
func globalProp(id, prop string) (bool, string) {
	var names []string
	switch id {
	case "app":
		names = []string{"theme", "mode", "systemMode", "systemLanguage", "language", "dark", "os"}
	case "window":
		names = []string{"width", "height", "minimized", "minimizable", "maximized", "maximizable", "resizable", "fullscreen"}
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
	// A builtin call is a value too: `width: max(120, min(window.width / 3, 480))`
	// is evaluated at runtime like any other expression.
	case *syntax.Ref, *syntax.Binary, *syntax.Unary, *syntax.Ternary, *syntax.Call:
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

// lenProp stands in for the argument of len and cap, where a list is a value
// like any other.
var lenProp = &Prop{Name: "len", Type: TypeNumber}

// builtinSigs is the arity of each Go builtin a design may call: formatf and t
// are ours, the rest are the pure ones. -1 means "any number".
var builtinSigs = map[string]struct {
	min, max int
	detail   string
}{
	"formatf": {1, -1, `formatf("%d items", count) — Go's fmt verbs`},
	"t":       {1, -1, `t("settings.theme") — a translation from the i18n folder`},
	"len":     {1, 1, "len(x) — the length of a string, a list or a map"},
	"cap":     {1, 1, "cap(x) — a design list has no spare capacity, so this is its length"},
	"min":     {1, -1, "min(a, b, …) — the smallest number or string"},
	"max":     {1, -1, "max(a, b, …) — the largest number or string"},
}

// checkBuiltin validates a call in a value position: the name is a builtin, the
// argument count fits, and each argument can be a value. len and cap accept a
// list, which no other value position does. A function a setup block declares is
// accepted too; its body and arguments are the interpreter's concern.
func (v *validator) checkBuiltin(e *syntax.Element, call *syntax.Call) {
	at := call.At
	end := at + syntax.Pos(len(call.Name)+2)
	if _, ok := v.setupFuncs[call.Name]; ok {
		for _, a := range call.Args {
			v.checkRefs(e, anyProp, a)
		}
		return
	}
	sig, ok := builtinSigs[call.Name]
	if !ok {
		names := make([]string, 0, len(builtinSigs))
		for n := range builtinSigs {
			names = append(names, n+"(…)")
		}
		sort.Strings(names)
		v.add(at, end,
			fmt.Sprintf("%s.%s: %q is not a builtin; a value can call %s", e.Name, condName(call), call.Name, strings.Join(names, ", ")),
			syntax.Suggest(call.Name, builtinNames()))
		return
	}
	if len(call.Args) < sig.min || (sig.max >= 0 && len(call.Args) > sig.max) {
		want := fmt.Sprintf("%d", sig.min)
		if sig.max < 0 {
			want = fmt.Sprintf("at least %d", sig.min)
		} else if sig.max != sig.min {
			want = fmt.Sprintf("%d to %d", sig.min, sig.max)
		}
		v.add(at, end, fmt.Sprintf("%s.%s: %s takes %s argument(s), got %d", e.Name, condName(call), call.Name, want, len(call.Args)), "")
		return
	}
	argProp := anyProp
	if call.Name == "len" || call.Name == "cap" {
		argProp = lenProp
	}
	for _, a := range call.Args {
		v.checkRefs(e, argProp, a)
	}
}

// SetupDiagnostics parses setup bodies with Go's parser and maps the errors back
// to their .goi positions. The interpreter still owns type and runtime errors;
// this covers the syntax the language server can report immediately.
func SetupDiagnostics(f *syntax.File) syntax.Diagnostics {
	if f == nil || len(f.Setups) == 0 {
		return nil
	}
	const prefix = "package setup\n"
	var out syntax.Diagnostics
	for _, st := range f.Setups {
		_, err := parser.ParseFile(token.NewFileSet(), "_.go", prefix+st.Code, parser.AllErrors)
		if err == nil {
			continue
		}
		list, ok := err.(scanner.ErrorList)
		if !ok {
			out = append(out, syntax.Diagnostic{Src: st.Src, Pos: st.Body, End: st.Body + 1, Message: err.Error()})
			continue
		}
		starts := setupLineStarts(st.Code)
		for _, item := range list {
			// go/parser sees `package setup` as line 1, so the body's first
			// line is line 2. Column is already a byte offset within that line.
			line := item.Pos.Line - 2
			if line < 0 {
				line = 0
			}
			base := 0
			if line < len(starts) {
				base = starts[line]
			}
			off := base + item.Pos.Column - 1
			pos := st.Body + syntax.Pos(off)
			out = append(out, syntax.Diagnostic{Src: st.Src, Pos: pos, End: pos + 1, Message: item.Msg})
		}
	}
	// Type-check the body with the compiler's own checker. It knows the
	// standard library, which is what a setup block may import.
	for _, st := range f.Setups {
		for _, item := range gosetup.AnalyzeWithBridge(st.Code, f.Vars, f.Shapes, st.Props).Diagnostics() {
			pos := st.Body + syntax.Pos(item.Offset)
			out = append(out, syntax.Diagnostic{Src: st.Src, Pos: pos, End: pos + 1, Message: item.Msg})
		}
	}
	return out
}

// setupLineStarts returns the byte offset of each line in a setup body.
func setupLineStarts(s string) []int {
	starts := []int{0}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// setupDecls collects the top-level functions and values a file's setup blocks
// declare. A broken block is reported by the interpreter at load; validation
// only needs the names it can read.
func setupDecls(f *syntax.File) (funcs map[string]*syntax.Type, values map[string]*syntax.Type) {
	if f == nil || len(f.Setups) == 0 {
		return nil, nil
	}
	funcs, values = map[string]*syntax.Type{}, map[string]*syntax.Type{}
	for _, st := range f.Setups {
		pf, err := parser.ParseFile(token.NewFileSet(), "_.go", "package setup\n"+st.Code, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range pf.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					var result *syntax.Type
					if d.Type.Results != nil && len(d.Type.Results.List) == 1 {
						result = goSetupType(d.Type.Results.List[0].Type)
					}
					funcs[d.Name.Name] = result
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						typ := goSetupType(vs.Type)
						if typ == nil {
							typ = goSetupValueType(vs.Values)
						}
						for _, name := range vs.Names {
							values[name.Name] = typ
						}
					}
				}
			}
		}
	}
	return funcs, values
}

// condName names the property a call was found in, for a diagnostic.
func condName(call *syntax.Call) string {
	if call.Name == "" {
		return "value"
	}
	return call.Name
}

// builtinNames lists the builtins, for suggestions.
func builtinNames() []string {
	out := make([]string, 0, len(builtinSigs))
	for n := range builtinSigs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// boolProp stands in for the property a block condition reads, so a reference
// in an `if` condition is type-checked exactly like a bool property value.
var boolProp = &Prop{Name: "condition", Type: TypeBool}

// condElement names a block condition in diagnostics: "{#if}.condition: ...".
var condElement = &syntax.Element{Name: "{#if}"}

// anyProp marks a reference whose target type is not checked, because a
// comparison decides the type itself (`r.title == "hide"`).
var anyProp = &Prop{Name: "value", Type: TypeString}

// walkBlockRefs checks the references inside block conditions and arms.
func (v *validator) walkBlockRefs(blk *syntax.Block) {
	if blk.Kind == syntax.BlockContinue || blk.Kind == syntax.BlockBreak {
		return
	}
	if blk.Kind == syntax.BlockFor {
		v.walkForRefs(blk)
		return
	}
	if blk.Kind == syntax.BlockSwitch {
		v.checkRefs(condElement, anyProp, blk.Subject)
	}
	for _, br := range blk.Branches {
		if br.Cond != nil {
			v.checkRefs(condElement, boolProp, br.Cond)
		}
		for _, val := range br.Vals {
			v.checkRefs(condElement, anyProp, val)
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

// walkForRefs checks the references inside a range loop with its loop variables
// in scope.
func (v *validator) walkForRefs(blk *syntax.Block) {
	keyShape, valShape, keyType, valType := v.checkForRange(blk)
	defer func() {
		v.scopes = v.scopes[:len(v.scopes)-1]
		v.loopTypes = v.loopTypes[:len(v.loopTypes)-1]
	}()
	v.scopes = append(v.scopes, map[string]*syntax.DataShape{})
	v.loopTypes = append(v.loopTypes, map[string]*syntax.Type{})
	top := v.scopes[len(v.scopes)-1]
	types := v.loopTypes[len(v.loopTypes)-1]
	for _, lv := range []struct {
		name string
		at   syntax.Pos
	}{{blk.LoopKey(), blk.KeyAt}, {blk.LoopVal(), blk.ValAt}} {
		if reservedValueName(lv.name) {
			v.add(lv.at, lv.at+syntax.Pos(len(lv.name)),
				fmt.Sprintf("%q is a reserved name; a loop variable cannot shadow it", lv.name), "")
		}
	}
	if k := blk.LoopKey(); k != "" {
		if keyShape != nil {
			top[k] = keyShape
		} else if keyType != nil {
			types[k] = keyType
		}
	}
	if k := blk.LoopVal(); k != "" {
		if valShape != nil {
			top[k] = valShape
		} else if valType != nil {
			types[k] = valType
		}
	}
	for _, br := range blk.Branches {
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
	case *syntax.Modifier:
		for _, entry := range x.Entries {
			v.checkRefs(e, pd, entry.Value)
			if entry.Cond != nil {
				v.checkRefs(e, boolProp, entry.Cond)
			}
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
	case *syntax.Call:
		v.checkBuiltin(e, x)
	case *syntax.Ref:
		if pd == nil {
			return
		}
		at := x.At
		end := x.End()
		free := pd == anyProp || pd == lenProp
		// A colour reference is checked by checkLiteral, which knows the two
		// namespaces; it is not an element id.
		if pd.Type == TypeColor && (x.ID == "theme" || x.ID == "customColor") {
			return
		}
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

// checkPropCall validates an event value inside a fragment: the call names a
// handler the props block declares, and the arguments fit its parameters.
func (v *validator) checkPropCall(e *syntax.Element, p *syntax.Property, call *syntax.Call, at, end syntax.Pos) {
	pr := v.file.Prop(call.Callee.Prop)
	if pr == nil {
		v.add(at, end,
			fmt.Sprintf("%s.%s: no prop %q", e.Name, p.Name, call.Callee.Prop),
			syntax.Suggest(call.Callee.Prop, declaredPropNames(v.file)))
		return
	}
	if pr.Type.Kind != syntax.TypeFunc {
		v.add(at, end,
			fmt.Sprintf("%s.%s: props.%s is not a handler; declare it as %s func(...)",
				e.Name, p.Name, call.Callee.Prop, call.Callee.Prop), "")
		return
	}
	v.checkCallArgs(e, p.Name, call, pr.Type.Args, end, nil)
}

// checkCallArgs checks a handler call's argument count and each argument's type
// against the declared parameters.
func (v *validator) checkCallArgs(e *syntax.Element, pname string, call *syntax.Call, params []syntax.Field, end syntax.Pos, ev *EventDef) {
	if len(call.Args) != len(params) {
		v.add(call.At, end,
			fmt.Sprintf("%s.%s: %s takes %d argument(s), got %d",
				e.Name, pname, call.Callee.Prop, len(params), len(call.Args)), "")
		return
	}
	for i, a := range call.Args {
		ft, _ := TypeOf(&params[i].Type)
		prop := &Prop{Name: pname, Type: ft}
		if id, ok := a.(*syntax.Ident); ok && id.Val == "event" && ev != nil {
			switch {
			case !ev.HasValue:
				v.add(a.Pos(), a.Pos()+1,
					fmt.Sprintf("%s.%s: %s carries no value, so event cannot be passed", e.Name, pname, pname), "")
			case !typesFit(ev.Type, ft):
				v.add(a.Pos(), a.Pos()+1,
					fmt.Sprintf("%s.%s: %s carries %s, which cannot feed %s",
						e.Name, pname, pname, typeName(ev.Type), typeName(ft)), "")
			}
			continue
		}
		if r, ok := a.(*syntax.Ref); ok && r.ID == "event" && ev != nil {
			fieldType, ok := eventFieldType(pname, r.Prop)
			if !ok {
				v.add(r.At, r.At+syntax.Pos(len(r.ID)+1+len(r.Prop)),
					fmt.Sprintf("%s.%s: %s has no field %q", e.Name, pname, pname, r.Prop),
					syntax.Suggest(r.Prop, eventFieldNames(pname)))
				continue
			}
			if !typesFit(fieldType, ft) {
				v.add(r.At, r.At+syntax.Pos(len(r.ID)+1+len(r.Prop)),
					fmt.Sprintf("%s.%s: event.%s is %s, which cannot feed %s",
						e.Name, pname, r.Prop, typeName(fieldType), typeName(ft)), "")
			}
			continue
		}
		v.checkLiteral(e, prop, a)
		v.checkRefs(e, prop, a)
	}
}

// eventFieldType is the type of one named field on an event payload.
func eventFieldType(event, field string) (ValueType, bool) {
	if event != "onScroll" {
		return 0, false
	}
	switch field {
	case "x", "y", "maxX", "maxY", "progress":
		return TypeNumber, true
	case "byUser":
		return TypeBool, true
	}
	return 0, false
}

// eventFieldNames lists the fields a diagnostic can suggest.
func eventFieldNames(event string) []string {
	if event == "onScroll" {
		return []string{"x", "y", "maxX", "maxY", "progress", "byUser"}
	}
	return nil
}

// handlerNames lists the bridge handlers, for suggestions.
func handlerNames(f *syntax.File) []string {
	out := make([]string, 0, len(f.Vars))
	for _, dv := range f.Vars {
		if dv.Type.Kind == syntax.TypeFunc {
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
	if call.Callee == nil {
		if _, ok := v.setupFuncs[call.Name]; !ok {
			v.add(call.At, call.At+syntax.Pos(len(call.Name)+2),
				fmt.Sprintf("%s.%s: %q is not a setup function; an event needs bridge., an element id or a setup function", e.Name, p.Name, call.Name), "")
			return
		}
		for _, a := range call.Args {
			v.checkRefs(e, anyProp, a)
		}
		return
	}
	at := call.Callee.At
	end := at + syntax.Pos(len(call.Callee.ID)+1+len(call.Callee.Prop))
	if call.Callee.ID == "props" {
		v.checkPropCall(e, p, call, at, end)
		return
	}
	if call.Callee.ID == "window" {
		if !WindowMethod(call.Callee.Prop) {
			v.add(at, end,
				fmt.Sprintf("%s.%s: window has no method %q; an event can call maximize, toggleMaximize, minimize, close or fullscreen",
					e.Name, p.Name, call.Callee.Prop),
				syntax.Suggest(call.Callee.Prop, WindowMethods))
			return
		}
		switch call.Callee.Prop {
		case "maximize", "fullscreen":
			if len(call.Args) != 1 {
				v.add(at, end, fmt.Sprintf("%s.%s: window.%s takes one bool",
					e.Name, p.Name, call.Callee.Prop), "")
				return
			}
			if _, ok := call.Args[0].(*syntax.Bool); !ok && !boolishValue(call.Args[0]) {
				v.add(at, end, fmt.Sprintf("%s.%s: window.%s takes a bool",
					e.Name, p.Name, call.Callee.Prop), "")
			}
		default:
			if len(call.Args) != 0 {
				v.add(at, end, fmt.Sprintf("%s.%s: window.%s takes no arguments",
					e.Name, p.Name, call.Callee.Prop), "")
			}
		}
		return
	}
	if call.Callee.ID != "bridge" && call.Callee.ID != "data" {
		// An event may drive an element by id: `onClick: spin.pause()`.
		if call.Callee == nil {
			v.add(at, end, fmt.Sprintf("%s.%s: a handler call needs bridge. or an element id", e.Name, p.Name), "")
			return
		}
		if call.Callee.ID == "props" {
			return
		}
		if !NodeMethod(call.Callee.Prop) {
			v.add(at, end,
				fmt.Sprintf("%s.%s: a handler call starts with bridge., or calls a method on an id",
					e.Name, p.Name),
				syntax.Suggest(call.Callee.Prop, NodeMethods))
			return
		}
		target, ok := v.elems[call.Callee.ID]
		if !ok {
			v.add(at, end,
				fmt.Sprintf("%s.%s: no element with id %q", e.Name, p.Name, call.Callee.ID), "")
			return
		}
		switch call.Callee.Prop {
		case "seek", "volume", "audioTrack":
			// A player's transport takes one number.
			if len(call.Args) != 1 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes one number",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "scrollTo":
			if len(call.Args) != 1 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes one number",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "autoScroll":
			if len(call.Args) != 1 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes one bool",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "autoScrollSmooth":
			if len(call.Args) != 2 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a bool and a duration",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			} else if _, ok := call.Args[1].(*syntax.Duration); !ok {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a duration such as 300ms as its second argument",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "scrollToSmooth":
			if len(call.Args) != 2 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a number and a duration",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			} else if _, ok := call.Args[1].(*syntax.Duration); !ok {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a duration such as 300ms as its second argument",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "scrollToNthChild":
			if len(call.Args) < 1 || len(call.Args) > 2 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a number and an optional start, center or end",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			} else if len(call.Args) == 2 && !scrollAlignValue(call.Args[1]) {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes start, center or end as its alignment",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "scrollToNthChildSmooth":
			if len(call.Args) != 2 && len(call.Args) != 3 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a number, an optional start, center or end, and a duration",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
				return
			}
			last := call.Args[len(call.Args)-1]
			if _, ok := last.(*syntax.Duration); !ok {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a duration such as 300ms as its last argument",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
				return
			}
			if len(call.Args) == 3 && !scrollAlignValue(call.Args[1]) {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes start, center or end as its alignment",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "scrollToChild":
			if len(call.Args) < 1 || len(call.Args) > 2 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes an id and an optional start, center or end",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			} else if len(call.Args) == 2 && !scrollAlignValue(call.Args[1]) {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes start, center or end as its alignment",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "scrollToChildSmooth":
			if len(call.Args) != 2 && len(call.Args) != 3 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes an id, an optional start, center or end, and a duration",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
				return
			}
			last := call.Args[len(call.Args)-1]
			if _, ok := last.(*syntax.Duration); !ok {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a duration such as 300ms as its last argument",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
				return
			}
			if len(call.Args) == 3 && !scrollAlignValue(call.Args[1]) {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes start, center or end as its alignment",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "setObscureText":
			// A password field's reveal button: `field.setObscureText(false)`.
			// The value is unchanged, so the argument is required.
			if def, ok := Lookup(target.Name); ok && def.Prop("obscureText") == nil {
				v.add(at, end,
					fmt.Sprintf("%s.%s: %s has no obscureText to set", e.Name, p.Name, target.Name), "")
				return
			}
			if len(call.Args) != 1 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes one bool",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
				return
			}
			if _, ok := call.Args[0].(*syntax.Bool); !ok && !boolishValue(call.Args[0]) {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a bool",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "clear":
			// A text input's value: `field.clear()`.
			switch target.Name {
			case "TextField", "SearchBar", "TextArea":
			default:
				v.add(at, end,
					fmt.Sprintf("%s.%s: only a text input has clear", e.Name, p.Name), "")
				return
			}
			if len(call.Args) != 0 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes no arguments",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "show":
			// A popover's panel: `pop.show(true)`.
			if def, ok := Lookup(target.Name); ok && def.Name != "Popover" {
				v.add(at, end,
					fmt.Sprintf("%s.%s: only a Popover has show", e.Name, p.Name), "")
				return
			}
			if len(call.Args) != 1 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes one bool",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
				return
			}
			if _, ok := call.Args[0].(*syntax.Bool); !ok && !boolishValue(call.Args[0]) {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a bool",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		case "expand":
			// `expand()` toggles and `expand(true)` sets, so no argument is
			// legal and the one that may follow is a bool.
			if def, ok := Lookup(target.Name); ok && def.Prop("expanded") == nil {
				v.add(at, end,
					fmt.Sprintf("%s.%s: %s has no expanded state to set", e.Name, p.Name, target.Name), "")
				return
			}
			if len(call.Args) > 1 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes no argument or one bool",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
				return
			}
			if len(call.Args) == 1 {
				if _, ok := call.Args[0].(*syntax.Bool); !ok && !boolishValue(call.Args[0]) {
					v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes a bool",
						e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
				}
			}
		default:
			if len(call.Args) != 0 {
				v.add(at, end, fmt.Sprintf("%s.%s: %s.%s takes no arguments",
					e.Name, p.Name, call.Callee.ID, call.Callee.Prop), "")
			}
		}
		return
	}
	dv := v.file.Var(call.Callee.Prop)
	if dv == nil {
		v.add(at, end, fmt.Sprintf("%s.%s: no handler %q", e.Name, p.Name, call.Callee.Prop),
			syntax.Suggest(call.Callee.Prop, handlerNames(v.file)))
		return
	}
	if dv.Type.Kind != syntax.TypeFunc {
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s.%s is a value, not a handler; declare it as %s func(...)",
				e.Name, p.Name, call.Callee.ID, call.Callee.Prop, call.Callee.Prop), "")
		return
	}
	v.checkCallArgs(e, p.Name, call, dv.Type.Args, end, &ev)
}

// checkDataRef validates `bridge.name` against the declared variable and the
// property it feeds.
func (v *validator) checkDataRef(e *syntax.Element, pd *Prop, x *syntax.Ref, at, end syntax.Pos, free bool) {
	dv := v.file.Var(x.Prop)
	if dv == nil {
		v.add(at, end,
			fmt.Sprintf("%s.%s: no bridge variable %q", e.Name, pd.Name, x.Prop),
			syntax.Suggest(x.Prop, varNames(v.file)))
		return
	}
	if dv.Type.Kind == syntax.TypeFunc {
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s.%s is a handler; call it in an event such as onClick: bridge.%s(...)",
				e.Name, pd.Name, x.ID, x.Prop, x.Prop), "")
		return
	}
	ft := &dv.Type
	if len(x.Path) > 0 {
		field, sh, missing := resolveFieldPath(v.file, ft, x.Path)
		if field == nil {
			v.add(at, end, pathDiagnostic(x, ft, sh, missing), pathSuggestion(sh, missing))
			return
		}
		ft = &field.Type
	}
	switch ft.Kind {
	case syntax.TypeFunc:
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s is a handler; call it in an event", e.Name, pd.Name, x.Text()), "")
		return
	case syntax.TypeSlice, syntax.TypeMap:
		if pd == lenProp {
			return // len and cap take a list
		}
		if pd.Type == TypeList && listSliceFits(*ft, pd.Elem) {
			return
		}
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s is not a value; walk it with for i, row := range %s",
				e.Name, pd.Name, x.Text(), x.Text()), "")
		return
	}
	vft, _ := TypeOf(ft)
	if !free && !typesFit(vft, pd.Type) {
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s is %s, not %s", e.Name, pd.Name, x.Text(), typeName(vft), typeName(pd.Type)), "")
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
	ft := &field.Type
	if len(x.Path) > 0 {
		next, nsh, missing := resolveFieldPath(v.file, ft, x.Path)
		if next == nil {
			v.add(at, end, pathDiagnostic(x, ft, nsh, missing), pathSuggestion(nsh, missing))
			return
		}
		ft = &next.Type
	}
	if (ft.Kind == syntax.TypeSlice || ft.Kind == syntax.TypeMap) && pd != lenProp {
		if pd.Type == TypeList && listSliceFits(*ft, pd.Elem) {
			return
		}
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s is not a value; walk it with for i, row := range %s",
				e.Name, pd.Name, x.Text(), x.Text()), "")
		return
	}
	vft, _ := TypeOf(ft)
	if !free && !typesFit(vft, pd.Type) {
		v.add(at, end,
			fmt.Sprintf("%s.%s: %s is %s, not %s", e.Name, pd.Name, x.Text(), typeName(vft), typeName(pd.Type)), "")
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
// listSliceFits reports whether a bridge slice can feed a list property: the
// slice's element type must be the list's element type. A coordinate list also
// accepts a slice of two-number slices, which is how a GIS export writes pairs.
func listSliceFits(t syntax.Type, elem ValueType) bool {
	if t.Kind != syntax.TypeSlice || t.Elem == nil {
		return false
	}
	if elem == TypeCoords {
		if t.Elem.Kind != syntax.TypeSlice || t.Elem.Elem == nil {
			return false
		}
		ft, ok := TypeOf(t.Elem.Elem)
		return ok && typesFit(ft, TypeNumber)
	}
	ft, ok := TypeOf(t.Elem)
	return ok && typesFit(ft, elem)
}

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
	// A scalar property takes any of the three, so a data field of any simple
	// type can fill a table cell's value.
	if to == TypeScalar {
		return from == TypeString || from == TypeNumber || from == TypeBool || from == TypeEnum
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
	case *syntax.Call:
		for _, a := range x.Args {
			if valueHasPercent(a) {
				return true
			}
		}
	case *syntax.Map:
		for _, entry := range x.Entries {
			if valueHasPercent(entry.Value) {
				return true
			}
		}
	case *syntax.Modifier:
		for _, entry := range x.Entries {
			if valueHasPercent(entry.Value) || valueHasPercent(entry.Cond) {
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
	case syntax.ModifierValueKind:
		return "a modifier"
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

// isChart reports whether an element name is one of the chart kinds, which are
// the only elements whose children are series.
func isChart(name string) bool { return componentdef.Chart(name) }

// listExample shows the shape a list property wants, so the diagnostic names
// the form instead of only rejecting the value.
func listExample(elem ValueType) string {
	switch elem {
	case TypeNumber:
		return "[42, 27, 18]"
	case TypeColor:
		return "[theme.primary, theme.tertiary, #c62828]"
	case TypeCoords:
		return "[[28.97, 41.01], [29.06, 41.08]]"
	}
	return `["Direct", "Search"]`
}

// listItemLen is how many source bytes an item covers, so a diagnostic can
// underline it. A literal with a known width uses it; everything else gets one
// byte, which still marks the spot.
func listItemLen(v syntax.Value) int {
	switch x := v.(type) {
	case *syntax.String:
		return len(x.Val) + 2
	case *syntax.Ident:
		return len(x.Val)
	case *syntax.Ref:
		return int(x.End() - x.At)
	}
	return 1
}

// scrollAlignValue reports whether a scroll alignment could be start, center or
// end at run time. An expression is left to the frame that evaluates it.
func scrollAlignValue(v syntax.Value) bool {
	name := ""
	switch x := v.(type) {
	case *syntax.Ident:
		name = x.Val
	case *syntax.String:
		name = x.Val
	default:
		return true
	}
	return name == "start" || name == "center" || name == "end"
}

// boolishValue reports whether a value could be a bool at run time: a literal
// bool, or an expression whose type the checker cannot know without running it.
// It is what lets `expand(split.expanded)` pass while `expand(3)` does not.
func boolishValue(v syntax.Value) bool {
	switch v.(type) {
	case *syntax.Ref, *syntax.Binary, *syntax.Unary, *syntax.Ternary, *syntax.Call:
		return true
	}
	return false
}

// goSetupType turns a Go type expression in a setup declaration into the
// design's type shape, so a block condition or a loop range can be checked
// without running the interpreter. nil means "not known here".
func goSetupType(e ast.Expr) *syntax.Type {
	switch x := e.(type) {
	case *ast.Ident:
		return &syntax.Type{Kind: syntax.TypeScalar, Name: x.Name}
	case *ast.ArrayType:
		elem := goSetupType(x.Elt)
		if elem == nil {
			return nil
		}
		return &syntax.Type{Kind: syntax.TypeSlice, Elem: elem}
	case *ast.MapType:
		key, ok := x.Key.(*ast.Ident)
		if !ok {
			return nil
		}
		elem := goSetupType(x.Value)
		if elem == nil {
			return nil
		}
		return &syntax.Type{Kind: syntax.TypeMap, Name: key.Name, Elem: elem}
	}
	return nil
}

// goSetupValueType infers the type of a setup value from its initializer when
// it has no written type: the common `var ready = true` or `var items =
// []string{}` forms.
func goSetupValueType(values []ast.Expr) *syntax.Type {
	if len(values) == 0 {
		return nil
	}
	switch x := values[0].(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.STRING:
			return &syntax.Type{Kind: syntax.TypeScalar, Name: "string"}
		case token.INT:
			return &syntax.Type{Kind: syntax.TypeScalar, Name: "int"}
		case token.FLOAT:
			return &syntax.Type{Kind: syntax.TypeScalar, Name: "float64"}
		}
	case *ast.Ident:
		if x.Name == "true" || x.Name == "false" {
			return &syntax.Type{Kind: syntax.TypeScalar, Name: "bool"}
		}
	case *ast.CompositeLit:
		return goSetupType(x.Type)
	}
	return nil
}
