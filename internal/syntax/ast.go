// Package syntax turns .goi source text into an immutable AST.
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
	// Vars is the `data { ... }` block: the app's variables, which Go sets and a
	// design reads in conditions, values and {#each} sources.
	Vars []DataVar
	// Shapes is one `data Name { ... }` per row type an {#each} can walk.
	Shapes []DataShape
	// Fragment records a `$children` slot. A file that has one is a fragment
	// too: it is opened on its own only to preview it, without children.
	Fragment bool
	Root     *Element
}

// IsFragment reports whether the file is meant to be imported rather than
// opened as a window: it declares props or a `$children` slot.
func (f *File) IsFragment() bool { return len(f.Props) > 0 || f.Fragment }

// Import is `import "./Sidebar.goi" as Sidebar`.
type Import struct {
	Path  string // as written, relative to the importing file
	Alias string // the element name that instantiates the fragment
	At    Pos
	// AliasAt is the position of the alias, for diagnostics.
	AliasAt Pos
}

// DataVar is one variable in `bridge { loading: bool }`.
type DataVar struct {
	Name   string
	NameAt Pos
	// List marks a collection: `tickets: [ticket]`. Elem is then the shape it
	// holds; for a scalar, Type names the value type.
	List bool
	Type string
	Elem string
	// Fn marks a handler: `onOpen: func(id: string)`. Args are its parameters,
	// in order, and Type is "func".
	Fn   bool
	Args []DataField
	// At is the type's position, for diagnostics.
	At Pos
}

// DataShape is `data ticket { title: string, unread: bool }`, the row type an
// {#each} walks.
type DataShape struct {
	Name   string
	NameAt Pos
	Fields []DataField
	// At is the opening brace, Close the closing one.
	At, Close Pos
}

// DataField is one field of a shape.
type DataField struct {
	Name   string
	NameAt Pos
	List   bool
	Type   string
	Elem   string
	Fn     bool
	Args   []DataField
	At     Pos
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

// Prop is one entry of a fragment's `props { ... }` block: a typed value the
// instance passes, with an optional default. The fragment reads it as
// `props.name`; a prop the instance leaves out is its default, or the type's
// zero value when it declares none.
type Prop struct {
	Name   string
	NameAt Pos
	// Type is one of bool, string, number, color.
	Type string
	// Default, when the declaration wrote `= value`.
	Default Value
	// At is the type's position, for diagnostics.
	At Pos
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

// Block kinds.
const (
	// BlockIf is `{#if cond} ... {:else if cond} ... {:else} ... {/if}`.
	BlockIf BlockKind = iota
	// BlockEach is `{#each rows as row} ... {:else} ... {/each}`. Source names
	// a declared list variable and Var is the loop variable.
	BlockEach
)

// Block is a control block inside an element body. Its branches are built and
// toggled at runtime; a design file declares no logic of its own.
type Block struct {
	Kind BlockKind
	// At is the position of the opening '{', for diagnostics.
	At Pos
	// Close is the position of the '{' of the closing tag.
	Close Pos
	// Src names the file this block came from.
	Src string
	// Branches holds the `{#if}`, `{:else if}` and `{:else}` arms in source
	// order. Only the last one may have a nil Cond, which is the else arm.
	Branches []Branch
	// Source and Var belong to BlockEach: the list variable and the loop
	// variable that names one row inside the arm.
	Source   string
	SourceAt Pos
	Var      string
	VarAt    Pos
}

// Branch is one arm of a block.
type Branch struct {
	// Cond is the arm's condition, or nil for `{:else}`.
	Cond Value
	// At is the position of the '{' that opened the arm.
	At Pos
	// Members are the arm's properties and elements, in source order.
	Members []Member
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
)

// Value is a property literal. The concrete type is one of *String,
// *Number, *Bool, *Color, *Ident, *Map, *Percent, *Ref or *Binary.
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

// Call is `bridge.onOpen(post.id)`, the whole value of an event property. The
// arguments are evaluated when the event fires, not every frame.
type Call struct {
	At     Pos
	Callee *Ref
	Args   []Value
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
