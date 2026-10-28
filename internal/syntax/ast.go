// Package syntax turns .ui source text into an immutable AST.
//
// The parser is deliberately small: a file is one element, an element has a
// name and braces, and members are either `name: value` properties or nested
// elements. Newlines are not tokens; the parser distinguishes a property from a
// child element by looking at the token after the identifier.
package syntax

// Pos is a byte offset into the source, converted to line/column for diagnostics.
type Pos int

// Pos returns the position as a Pos.
func (p Pos) Pos() Pos { return p }

// File is a parsed .ui file. Exactly one root element.
type File struct {
	Root *Element
}

// Element is `Name { members... }`.
type Element struct {
	Name   string
	NameAt Pos
	Open   Pos // position of '{'
	Close  Pos // position of '}'
	// Members preserves source order so instantiation and error messages are
	// predictable. Use Props/Children to read them by kind.
	Members []Member
}

// Props returns the element's properties in source order.
func (e *Element) Props() []*Property {
	var out []*Property
	for _, m := range e.Members {
		if p, ok := m.(*Property); ok {
			out = append(out, p)
		}
	}
	return out
}

// Children returns the element's child elements in source order.
func (e *Element) Children() []*Element {
	var out []*Element
	for _, m := range e.Members {
		if c, ok := m.(*Element); ok {
			out = append(out, c)
		}
	}
	return out
}

// Prop returns the last property with the given name, or nil.
func (e *Element) Prop(name string) *Property {
	var found *Property
	for _, m := range e.Members {
		if p, ok := m.(*Property); ok && p.Name == name {
			found = p
		}
	}
	return found
}

// Member is a *Property or *Element.
type Member interface{ member() }

func (*Property) member() {}
func (*Element) member()  {}

// Property is `Name: Value`.
type Property struct {
	Name   string
	NameAt Pos
	Value  Value
}

// ValueKind identifies the type of a literal.
type ValueKind int

// Value kinds.
const (
	StringKind ValueKind = iota
	NumberKind
	BoolKind
	ColorKind
	IdentKind
	MapKind
)

// Value is a property literal. The concrete type is one of *String,
// *Number, *Bool, *Color, *Ident or *Map.
type Value interface {
	Kind() ValueKind
	Pos() Pos
}

// String is a quoted string literal.
type String struct {
	At  Pos
	Val string
}

// Number is an integer or floating point literal.
type Number struct {
	At  Pos
	Val float64
}

// Bool is `true` or `false`.
type Bool struct {
	At  Pos
	Val bool
}

// Color is a `#rgb`, `#rrggbb` or `#rrggbbaa` literal, already expanded to
// 0xRRGGBBAA.
type Color struct {
	At   Pos
	RGBA uint32
}

// Ident is a bare identifier, used for enums and for the string-like `id`.
type Ident struct {
	At  Pos
	Val string
}

// Map is `{ key: value, ... }`, the responsive form of a property value.
type Map struct {
	At      Pos
	Entries []MapEntry
}

// MapEntry is one `key: value` pair inside a Map.
type MapEntry struct {
	Key   string
	KeyAt Pos
	Value Value
}

// Kind implementations.

func (s *String) Kind() ValueKind { return StringKind }
func (n *Number) Kind() ValueKind { return NumberKind }
func (b *Bool) Kind() ValueKind   { return BoolKind }
func (c *Color) Kind() ValueKind  { return ColorKind }
func (i *Ident) Kind() ValueKind  { return IdentKind }
func (m *Map) Kind() ValueKind    { return MapKind }

// Pos implementations.

func (s *String) Pos() Pos { return s.At }
func (n *Number) Pos() Pos { return n.At }
func (b *Bool) Pos() Pos   { return b.At }
func (c *Color) Pos() Pos  { return c.At }
func (i *Ident) Pos() Pos  { return i.At }
func (m *Map) Pos() Pos    { return m.At }
