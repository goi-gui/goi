// Package schema defines every element and property the .ui language accepts,
// along with the literal type each property requires. It is the single place
// that knows the vocabulary; the parser stays generic.
package schema

import (
	"fmt"
	"sort"
)

// ValueType is the literal type a property demands.
type ValueType int

// Value types. Enum and String accept an Ident and a String respectively; the
// parser keeps them distinct so diagnostics can name the expected form.
const (
	TypeString ValueType = iota
	TypeNumber
	TypeBool
	TypeColor
	TypeEnum
	TypeID
)

func (t ValueType) String() string {
	switch t {
	case TypeString:
		return "string"
	case TypeNumber:
		return "number"
	case TypeBool:
		return "bool"
	case TypeColor:
		return "color"
	case TypeEnum:
		return "identifier"
	case TypeID:
		return "id"
	}
	return "unknown"
}

// DirtyKind describes what a property affects when it changes.
type DirtyKind int

// Dirty kinds.
const (
	DirtyNone DirtyKind = iota
	DirtyPaint
	DirtyLayout
)

// Prop describes one property of one element.
type Prop struct {
	Name    string
	Type    ValueType
	Dirty   DirtyKind
	Enum    []string // valid values for TypeEnum
	Default any      // Value is string/float64/bool/uint32/enum string
	// Common marks properties every element accepts.
	Common bool
}

// Element describes one element type.
type Element struct {
	Name     string
	Children bool // may contain child elements
	Attrs    []Prop
}

// Prop returns the named property, or nil.
func (e *Element) Prop(name string) *Prop {
	for i := range e.Attrs {
		if e.Attrs[i].Name == name {
			return &e.Attrs[i]
		}
	}
	return nil
}

var common = []Prop{
	{Name: "id", Type: TypeID, Dirty: DirtyNone, Common: true},
	{Name: "width", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true}, // -1 = auto
	{Name: "height", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "minWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "maxWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "margin", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "padding", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "background", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	{Name: "borderColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	{Name: "borderWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "radius", Type: TypeNumber, Dirty: DirtyPaint, Default: 0.0, Common: true},
	{Name: "visible", Type: TypeBool, Dirty: DirtyLayout, Default: true, Common: true},
	{Name: "flex", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "alignSelf", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "", Common: true},
}

func withCommon(extra ...Prop) []Prop {
	out := make([]Prop, len(common), len(common)+len(extra))
	copy(out, common)
	return append(out, extra...)
}

var elements = []Element{
	{Name: "Window", Children: true, Attrs: withCommon(
		Prop{Name: "title", Type: TypeString, Dirty: DirtyNone, Default: ""},
	)},
	{Name: "Column", Children: true, Attrs: withCommon(
		Prop{Name: "spacing", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
		Prop{Name: "align", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
	)},
	{Name: "Row", Children: true, Attrs: withCommon(
		Prop{Name: "spacing", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
		Prop{Name: "align", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
	)},
	{Name: "Stack", Children: true, Attrs: withCommon(
		Prop{Name: "align", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
	)},
	{Name: "Rectangle", Children: false, Attrs: withCommon()},
	{Name: "Text", Children: false, Attrs: withCommon(
		Prop{Name: "text", Type: TypeString, Dirty: DirtyLayout, Default: ""},
		Prop{Name: "fontSize", Type: TypeNumber, Dirty: DirtyLayout, Default: 16.0},
		Prop{Name: "color", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0x000000FF)},
		Prop{Name: "align", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end"}, Default: "start"},
		Prop{Name: "wrap", Type: TypeBool, Dirty: DirtyLayout, Default: false},
	)},
	{Name: "Button", Children: false, Attrs: withCommon(
		Prop{Name: "text", Type: TypeString, Dirty: DirtyLayout, Default: ""},
		Prop{Name: "fontSize", Type: TypeNumber, Dirty: DirtyLayout, Default: 16.0},
		Prop{Name: "color", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0xFFFFFFFF)},
		Prop{Name: "hoverBackground", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
	)},
	{Name: "TextInput", Children: false, Attrs: withCommon(
		Prop{Name: "text", Type: TypeString, Dirty: DirtyLayout, Default: ""},
		Prop{Name: "placeholder", Type: TypeString, Dirty: DirtyLayout, Default: ""},
		Prop{Name: "fontSize", Type: TypeNumber, Dirty: DirtyLayout, Default: 16.0},
		Prop{Name: "color", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0x000000FF)},
	)},
}

var elementIndex = func() map[string]*Element {
	m := make(map[string]*Element, len(elements))
	for i := range elements {
		m[elements[i].Name] = &elements[i]
	}
	return m
}()

// Lookup returns the element definition by name.
func Lookup(name string) (*Element, bool) {
	e, ok := elementIndex[name]
	return e, ok
}

// ElementNames returns all element names, sorted, for suggestions.
func ElementNames() []string {
	names := make([]string, 0, len(elements))
	for _, e := range elements {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	return names
}

// RootNames returns the elements allowed as a file root.
func RootNames() []string { return []string{"Window"} }

// String formats an element definition for documentation.
func (e *Element) String() string { return fmt.Sprintf("element %s", e.Name) }
