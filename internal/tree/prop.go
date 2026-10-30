package tree

import "github.com/ksckaan1/goi/internal/syntax"

// DirtyKind says what a change affects.
type DirtyKind uint8

// Dirty kinds. Layout implies paint.
const (
	DirtyPaint DirtyKind = iota
	DirtyLayout
)

// Prop is a typed, observable property cell. Set is a no-op when the value does
// not change, so assigning the same value in a handler costs nothing.
//
// It is published as ui.Prop[T].
type Prop[T comparable] struct {
	base  *NodeBase
	name  string
	dirty DirtyKind
	val   T
	// goSet marks a value written by Go code, so hot reload knows it should not
	// be reverted to the .goi literal.
	goSet bool
}

func newProp[T comparable](owner *NodeBase, name string, dirty DirtyKind, def T) Prop[T] {
	return Prop[T]{base: owner, name: name, dirty: dirty, val: def}
}

// Get returns the current value.
func (p *Prop[T]) Get() T { return p.val }

// Set stores v and marks the owner dirty if it changed.
func (p *Prop[T]) Set(v T) {
	if p.val == v {
		return
	}
	p.val = v
	p.goSet = true
	if p.base != nil {
		p.base.mark(p.dirty)
	}
}

// Event is a typed event with any number of Go subscribers.
//
// It is published as ui.Event[E].
type Event[E any] struct {
	handlers []func(E)
}

// On subscribes fn and returns an unsubscribe function.
func (e *Event[E]) On(fn func(E)) (off func()) {
	e.handlers = append(e.handlers, fn)
	i := len(e.handlers) - 1
	return func() {
		if i < len(e.handlers) && e.handlers[i] != nil {
			e.handlers[i] = nil
		}
	}
}

// Emit calls every subscriber in subscription order. A nil handler (someone
// unsubscribed) is skipped.
func (e *Event[E]) Emit(v E) {
	for _, fn := range e.handlers {
		if fn != nil {
			fn(v)
		}
	}
}

// Len reports the number of subscriptions, including removed ones.
func (e *Event[E]) Len() int { return len(e.handlers) }

// NamedProp is a type-erased view of one property, used by the builder and by
// hot reload. Nodes construct these once; no reflection is involved.
type NamedProp struct {
	Name  string
	Dirty DirtyKind
	Get   func() any
	// SetLiteral applies a parsed literal, reporting false when the literal is
	// not valid for the property type.
	SetLiteral func(syntax.Value) bool
	// CopyFrom copies the value of another node's property of the same name and
	// type. Hot reload uses it to carry Go-set values across a reload.
	CopyFrom func(NamedProp) bool
	// SetColor writes a resolved 0xRRGGBBAA without going through a literal.
	// Only color properties set it; the theme resolver uses it.
	SetColor func(uint32)
	// goSet reports whether Go changed this property.
	goSet func() bool
}

// GoSet reports whether Go code changed this property since the tree was built.
func (p NamedProp) GoSet() bool {
	return p.goSet != nil && p.goSet()
}

// StringProp builds a NamedProp for a string cell.
func StringProp(b *NodeBase, name string, p *Prop[string]) NamedProp {
	wire(b, name, p)
	return NamedProp{
		Name: name,
		Get:  func() any { return p.val },
		SetLiteral: func(v syntax.Value) bool {
			switch s := v.(type) {
			case *syntax.String:
				p.val = s.Val
			case *syntax.Ident:
				p.val = s.Val
			default:
				return false
			}
			return true
		},
		CopyFrom: func(src NamedProp) bool {
			v, ok := src.Get().(string)
			if !ok {
				return false
			}
			p.val = v
			return true
		},
		goSet: func() bool { return p.goSet },
	}
}

// NumberProp builds a NamedProp for a float64 cell.
func NumberProp(b *NodeBase, name string, dirty DirtyKind, p *Prop[float64]) NamedProp {
	wire(b, name, p)
	return NamedProp{
		Name:  name,
		Dirty: dirty,
		Get:   func() any { return p.val },
		SetLiteral: func(v syntax.Value) bool {
			n, ok := v.(*syntax.Number)
			if !ok {
				return false
			}
			p.val = n.Val
			return true
		},
		CopyFrom: func(src NamedProp) bool {
			v, ok := src.Get().(float64)
			if !ok {
				return false
			}
			p.val = v
			return true
		},
		goSet: func() bool { return p.goSet },
	}
}

// BoolProp builds a NamedProp for a bool cell.
func BoolProp(b *NodeBase, name string, dirty DirtyKind, p *Prop[bool]) NamedProp {
	wire(b, name, p)
	return NamedProp{
		Name:  name,
		Dirty: dirty,
		Get:   func() any { return p.val },
		SetLiteral: func(v syntax.Value) bool {
			b, ok := v.(*syntax.Bool)
			if !ok {
				return false
			}
			p.val = b.Val
			return true
		},
		CopyFrom: func(src NamedProp) bool {
			v, ok := src.Get().(bool)
			if !ok {
				return false
			}
			p.val = v
			return true
		},
		goSet: func() bool { return p.goSet },
	}
}

// ColorProp builds a NamedProp for a uint32 RGBA cell.
func ColorProp(b *NodeBase, name string, dirty DirtyKind, p *Prop[uint32]) NamedProp {
	wire(b, name, p)
	return NamedProp{
		Name:  name,
		Dirty: dirty,
		Get:   func() any { return p.val },
		SetLiteral: func(v syntax.Value) bool {
			switch c := v.(type) {
			case *syntax.Color:
				b.clearColorRole(name)
				p.val = c.RGBA
			case *syntax.String:
				rgba, ok := ParseHexColor(c.Val)
				if !ok {
					return false
				}
				b.clearColorRole(name)
				p.val = rgba
			case *syntax.Ident:
				// A bare word is a theme role, resolved by ResolveColors.
				if !b.setColorRole(name, c.Val) {
					return false
				}
			default:
				return false
			}
			return true
		},
		SetColor: func(rgba uint32) { p.val = rgba },
		CopyFrom: func(src NamedProp) bool {
			v, ok := src.Get().(uint32)
			if !ok {
				return false
			}
			p.val = v
			return true
		},
		goSet: func() bool { return p.goSet },
	}
}

// EnumProp builds a NamedProp whose literal must be one of allowed.
func EnumProp(b *NodeBase, name string, dirty DirtyKind, p *Prop[string], allowed ...string) NamedProp {
	wire(b, name, p)
	return NamedProp{
		Name:  name,
		Dirty: dirty,
		Get:   func() any { return p.val },
		SetLiteral: func(v syntax.Value) bool {
			var s string
			switch x := v.(type) {
			case *syntax.Ident:
				s = x.Val
			case *syntax.String:
				s = x.Val
			default:
				return false
			}
			for _, a := range allowed {
				if s == a {
					p.val = s
					return true
				}
			}
			return false
		},
		CopyFrom: func(src NamedProp) bool {
			v, ok := src.Get().(string)
			if !ok {
				return false
			}
			for _, a := range allowed {
				if v == a {
					p.val = v
					return true
				}
			}
			return false
		},
		goSet: func() bool { return p.goSet },
	}
}

// wire connects a property cell to its owner so Set can mark the tree dirty.
// It runs when the node's table is first built, before any literal is applied.
func wire[T comparable](b *NodeBase, name string, p *Prop[T]) {
	p.base = b
	p.name = name
}
