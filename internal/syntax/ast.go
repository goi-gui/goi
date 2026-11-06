// Package syntax turns .goi source text into an immutable AST.
//
// The parser is deliberately small: a file is one element, an element has a
// name and braces, and members are either `name: value` properties or nested
// elements. Newlines are not tokens; the parser distinguishes a property from a
// child element by looking at the token after the identifier.
package syntax

import "strings"

// Pos is a byte offset into the source, converted to line/column for diagnostics.
type Pos int

// Pos returns the position as a Pos.
func (p Pos) Pos() Pos { return p }

// File is a parsed .goi file. Exactly one root element, after an optional
// preamble of `import`, `props`, `bridge` and `type` declarations.
type File struct {
	// Src is the path the file was read from, for diagnostics. Empty for a file
	// parsed from memory.
	Src string
	// Imports lists the fragments this file pulls in, in source order.
	Imports []Import
	// Props declares the values a fragment expects from its instance. A file
	// with props is a fragment: its root may be any element, not just Window.
	Props []Prop
	// Vars is the `bridge { ... }` block: the app's variables, which Go sets and
	// a design reads in conditions, values and for loops.
	Vars []DataVar
	// Shapes is one `type Name { ... }` per row type a for loop can walk.
	Shapes []DataShape
	// Fragment records a `$children` slot. A file that has one is a fragment
	// too: it is opened on its own only to preview it, without children.
	Fragment bool
	Root     *Element
	// Comments lists every comment in source order. The parser keeps them only
	// for the formatter; nothing downstream reads them.
	Comments []Comment
	// PropsAt and BridgeAt are the positions of the `props` and `bridge`
	// keywords, so a comment before the block stays outside it.
	PropsAt, BridgeAt Pos
}

// Comment is one `// …` or `/* … */` with its span in the source.
type Comment struct {
	At, End Pos
	Text    string
	// Block distinguishes `/* … */` from a line comment.
	Block bool
}

// CommentAt returns the comment that starts at pos.
func (f *File) CommentAt(pos Pos) *Comment {
	if f == nil {
		return nil
	}
	for i := range f.Comments {
		if f.Comments[i].At == pos {
			return &f.Comments[i]
		}
	}
	return nil
}

// IsFragment reports whether the file is meant to be imported rather than
// opened as a window: it declares props or a `$children` slot.
func (f *File) IsFragment() bool { return len(f.Props) > 0 || f.Fragment }

// Import is `import Sidebar "./Sidebar.goi"`.
type Import struct {
	Path  string // as written, relative to the importing file
	Alias string // the element name that instantiates the fragment
	At    Pos
	// AliasAt is the position of the alias, for diagnostics.
	AliasAt Pos
}

// TypeKind is the shape of a declared type.
type TypeKind int

// Type kinds. A type is written the way Go writes it.
const (
	// TypeScalar is a primitive such as `string`, `int` or `float64`.
	TypeScalar TypeKind = iota
	// TypeSlice is `[]T`.
	TypeSlice
	// TypeMap is `map[K]V`.
	TypeMap
	// TypeFunc is `func(a string) bool`.
	TypeFunc
)

// Type is a Go-like type expression: `string`, `[]User`, `map[string]int` or
// `func(id string)`.
type Type struct {
	Kind TypeKind
	// Name is the scalar's name, or the map's key type.
	Name string
	// Elem is the slice element type or the map value type.
	Elem *Type
	// Args are a func's parameters.
	Args []Field
	// At is the position of the type as written, for diagnostics.
	At Pos
}

// IsScalar reports whether the type is a named primitive.
func (t *Type) IsScalar() bool { return t != nil && t.Kind == TypeScalar }

// Scalar returns the scalar name, or "" for a composite.
func (t *Type) Scalar() string {
	if t == nil || t.Kind != TypeScalar {
		return ""
	}
	return t.Name
}

// String writes the type back the way the file spells it.
func (t *Type) String() string {
	if t == nil {
		return "?"
	}
	switch t.Kind {
	case TypeSlice:
		return "[]" + t.Elem.String()
	case TypeMap:
		return "map[" + t.Name + "]" + t.Elem.String()
	case TypeFunc:
		var b strings.Builder
		b.WriteString("func(")
		for i, a := range t.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(a.Name + " " + a.Type.String())
		}
		b.WriteString(")")
		return b.String()
	}
	return t.Name
}

// Field is one declaration: a bridge variable, a props entry, a shape field or
// a handler parameter. The four are aliases of this type, so one parser, one
// validator and one completion path serve all of them.
type Field struct {
	Name   string
	NameAt Pos
	Type   Type
	// At is the type's position, for diagnostics.
	At Pos
}

// DataVar is one variable in `bridge { title string }`.
type DataVar = Field

// DataField is one field of a shape, or a handler parameter.
type DataField = Field

// Prop is one entry of a fragment's `props { title string }` block.
type Prop = Field

// DataShape is `type ticket { title string }`, the row type a for loop walks.
type DataShape struct {
	Name   string
	NameAt Pos
	Fields []Field
	// At is the opening brace, Close the closing one.
	At, Close Pos
}

// Shape returns the named shape, or nil.
func (f *File) Shape(name string) *DataShape {
	if f == nil {
		return nil
	}
	for i := range f.Shapes {
		if f.Shapes[i].Name == name {
			return &f.Shapes[i]
		}
	}
	return nil
}

// Var returns the named variable, or nil.
func (f *File) Var(name string) *DataVar {
	if f == nil {
		return nil
	}
	for i := range f.Vars {
		if f.Vars[i].Name == name {
			return &f.Vars[i]
		}
	}
	return nil
}

// Prop returns the named prop, or nil.
func (f *File) Prop(name string) *Prop {
	if f == nil {
		return nil
	}
	for i := range f.Props {
		if f.Props[i].Name == name {
			return &f.Props[i]
		}
	}
	return nil
}

// BlockKind identifies a control block.
type BlockKind int

// Block kinds. Both are written the way Go writes them.
const (
	// BlockIf is `if cond { ... } else if cond { ... } else { ... }`.
	BlockIf BlockKind = iota
	// BlockFor is `for i, v := range list { ... }`.
	BlockFor
	// BlockSwitch is `switch expr { case a, b: ... default: ... }`.
	BlockSwitch
)

// Block is a control block inside an element body. Its arms are built and
// toggled at runtime; a design file declares no logic of its own.
type Block struct {
	Kind BlockKind
	// At is the position of the `if` or `for` keyword, for diagnostics.
	At Pos
	// Close is the position of the '}' that ends the block.
	Close Pos
	// Src names the file this block came from.
	Src string
	// Branches holds the `if`, `else if` and `else` arms in source order. Only
	// the last one may have a nil Cond, which is the else arm. A for block has
	// exactly one arm with a nil Cond; a switch block's arms carry Vals instead,
	// and its default arm has neither.
	Branches []Branch
	// KeyVar, ValVar and Range belong to BlockFor: `for key, val := range X`.
	// A name is "" when the design did not write it and "_" when it is
	// deliberately ignored.
	KeyVar string
	KeyAt  Pos
	ValVar string
	ValAt  Pos
	// Range is the value the loop walks: a list reference, a map reference or
	// a number. RangeAt is its position.
	Range   Value
	RangeAt Pos
	// Subject and SubjectAt belong to BlockSwitch: the value every case arm is
	// compared against.
	Subject   Value
	SubjectAt Pos
}

// Branch is one arm of a block.
type Branch struct {
	// Cond is the arm's condition, or nil for `else`.
	Cond Value
	// Vals are a switch arm's case values, in source order. Nil on the default
	// arm, and on every arm of an if or a for.
	Vals []Value
	// At is the position of the arm's '{', or of its `case`/`default` keyword.
	At Pos
	// Members are the arm's properties, elements and blocks, in source order.
	Members []Member
}

// LoopKey is the name the key is bound to, or "" when it is ignored.
func (b *Block) LoopKey() string {
	if b == nil || b.KeyVar == "_" {
		return ""
	}
	return b.KeyVar
}

// LoopVal is the name the value is bound to, or "" when it is ignored.
func (b *Block) LoopVal() string {
	if b == nil || b.ValVar == "_" {
		return ""
	}
	return b.ValVar
}

// Element is `Name { members... }`.
type Element struct {
	Name   string
	NameAt Pos
	Open   Pos // position of '{'
	Close  Pos // position of '}'
	// Src names the file this element came from. Pos values on this element are
	// offsets into that file, not into the file being built.
	Src string
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

// WalkElements calls fn for e and every element below it, including the ones
// inside block branches, parents before children.
func WalkElements(e *Element, fn func(*Element)) {
	if e == nil {
		return
	}
	fn(e)
	for _, m := range e.Members {
		switch x := m.(type) {
		case *Element:
			WalkElements(x, fn)
		case *Block:
			walkBlockElements(x, fn)
		}
	}
}

// walkBlockElements is WalkElements for a block that is itself a member.
func walkBlockElements(b *Block, fn func(*Element)) {
	for _, br := range b.Branches {
		for _, m := range br.Members {
			switch x := m.(type) {
			case *Element:
				WalkElements(x, fn)
			case *Block:
				walkBlockElements(x, fn)
			}
		}
	}
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

// Member is a *Property, an *Element or a *Block.
type Member interface{ member() }

func (*Property) member() {}
func (*Element) member()  {}
func (*Block) member()    {}

// Property is `Name: Value`.
type Property struct {
	Name   string
	NameAt Pos
	Value  Value
	// Src names the file this property was written in. It differs from the
	// element's Src when an instance overrides a fragment property.
	Src string
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
	PercentKind
	RefKind
	BinaryKind
	UnaryKind
	TernaryKind
	CallKind
	ShadowKind
)

// Value is a property literal. The concrete type is one of *String,
// *Number, *Bool, *Color, *Ident, *Map, *Percent, *Ref, *Binary or *Shadow.
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

// Percent is `<number>%`, relative to the space the parent offers. Only width
// and height accept it.
type Percent struct {
	At  Pos
	Val float64
}

// Ref is `id.property`, a read of another element's property.
type Ref struct {
	At   Pos
	ID   string
	Prop string
}

// Call is a function call: `bridge.onOpen(post.id)` as the whole value of an
// event property, or a builtin such as `formatf("%d items", count)` in a value.
// A handler call's arguments are evaluated when the event fires.
type Call struct {
	At     Pos
	Callee *Ref
	// Name is the bare function name for a builtin call (`formatf`), and empty
	// when Callee names a bridge handler.
	Name string
	Args []Value
}

// Binary is an expression: Left Op Right. Op is one of "+", "-", "*", "/",
// "==", "!=", "<", "<=", ">", ">=", "&&", "||".
type Binary struct {
	At    Pos
	Op    string
	Left  Value
	Right Value
}

// Unary is `!x` or `-x`.
type Unary struct {
	At Pos
	Op string
	X  Value
}

// Shadow is the value of a `shadow:` property: one or more layers separated by
// commas. It is written the way CSS writes a shadow, without quotes:
//
//	shadow: 2 2 5 primary, 0 0 8 #FF0000 inset
//
// A layer is `x y [blur [spread]] [color] [inset]`, so the examples in the docs
// read exactly like the CSS it borrows from.
type Shadow struct {
	At     Pos
	Layers []ShadowLayer
}

// ShadowLayer is one shadow in a Shadow value. Zero Blur is a hard edge, zero
// Spread keeps the shape's size, and a nil Color means the theme's shadow
// colour.
type ShadowLayer struct {
	At           Pos
	X, Y         float64
	Blur, Spread float64
	Color        Value // *Color or *Ident; nil for the theme's shadow colour
	// Inset is the trailing keyword: the shadow is drawn inside the shape, the
	// way CSS box-shadow paints it.
	Inset bool
}

// Ternary is `cond ? then : else`.
type Ternary struct {
	At         Pos
	Cond       Value
	Then, Else Value
}

// Kind implementations.

func (s *String) Kind() ValueKind  { return StringKind }
func (n *Number) Kind() ValueKind  { return NumberKind }
func (b *Bool) Kind() ValueKind    { return BoolKind }
func (c *Color) Kind() ValueKind   { return ColorKind }
func (i *Ident) Kind() ValueKind   { return IdentKind }
func (m *Map) Kind() ValueKind     { return MapKind }
func (p *Percent) Kind() ValueKind { return PercentKind }
func (r *Ref) Kind() ValueKind     { return RefKind }
func (b *Binary) Kind() ValueKind  { return BinaryKind }
func (u *Unary) Kind() ValueKind   { return UnaryKind }
func (t *Ternary) Kind() ValueKind { return TernaryKind }
func (c *Call) Kind() ValueKind    { return CallKind }
func (s *Shadow) Kind() ValueKind  { return ShadowKind }

// Pos implementations.

func (s *String) Pos() Pos  { return s.At }
func (n *Number) Pos() Pos  { return n.At }
func (b *Bool) Pos() Pos    { return b.At }
func (c *Color) Pos() Pos   { return c.At }
func (i *Ident) Pos() Pos   { return i.At }
func (m *Map) Pos() Pos     { return m.At }
func (p *Percent) Pos() Pos { return p.At }
func (r *Ref) Pos() Pos     { return r.At }
func (b *Binary) Pos() Pos  { return b.At }
func (u *Unary) Pos() Pos   { return u.At }
func (t *Ternary) Pos() Pos { return t.At }
func (c *Call) Pos() Pos    { return c.At }
func (s *Shadow) Pos() Pos  { return s.At }
