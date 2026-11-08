package tree

import (
	"strconv"

	"github.com/goi-gui/goi/internal/csscolor"
	"github.com/goi-gui/goi/internal/syntax"
)

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
		// A colour written by Go wins over the role the design named, otherwise
		// the next ResolveColors would put the scheme's colour back.
		p.base.clearColorRole(p.name)
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
// hot reload. It holds the cell itself rather than closures over it: a tree
// builds one of these per property of every node, so the closure set used to
// cost a few hundred bytes and five allocations per property, which for a large
// design was most of its memory. No reflection is involved either way.
type NamedProp struct {
	Name  string
	Dirty DirtyKind
	kind  propKind
	// cell is the typed property cell, always a pointer: *Prop[float64],
	// *Prop[string], *Prop[bool], *Prop[uint32] or *ShadowProp depending on
	// kind. One field instead of one per type keeps the entry at 48 bytes; a
	// node carries dozens of them and they were most of its memory.
	cell any
	// enum lists the values an enum property accepts. It points at the
	// caller's slice rather than copying its header, which saves two words per
	// property and there are dozens on every node.
	enum *[]string
}

type propKind uint8

const (
	propNumber propKind = iota
	propString
	propBool
	propColor
	propEnum
	propShadow
	// propScalar writes a string cell from a string, number or bool literal: a
	// table cell's value takes whatever the row's data holds.
	propScalar
)

// Get returns the value as an interface, boxing it. The expression layer reads
// properties a frame at a time, so prefer Same where a comparison is enough.
func (p *NamedProp) Get() any {
	switch p.kind {
	case propNumber:
		return p.cell.(*Prop[float64]).val
	case propString, propEnum, propScalar:
		return p.cell.(*Prop[string]).val
	case propBool:
		return p.cell.(*Prop[bool]).val
	case propColor:
		return p.cell.(*Prop[uint32]).val
	case propShadow:
		sp := p.cell.(*ShadowProp)
		out := make([]ShadowLayer, len(sp.layers))
		copy(out, sp.layers)
		return out
	}
	return nil
}

// Same reports whether v already is this property's value, without boxing the
// current one.
func (p *NamedProp) Same(v any) bool {
	switch p.kind {
	case propNumber:
		f, ok := v.(float64)
		return ok && p.cell.(*Prop[float64]).val == f
	case propString, propEnum, propScalar:
		s, ok := v.(string)
		return ok && p.cell.(*Prop[string]).val == s
	case propBool:
		b, ok := v.(bool)
		return ok && p.cell.(*Prop[bool]).val == b
	case propColor:
		c, ok := v.(uint32)
		return ok && p.cell.(*Prop[uint32]).val == c
	}
	return false
}

// SetLiteral applies a parsed literal, reporting false when it is not valid for
// the property type.
func (p *NamedProp) SetLiteral(v syntax.Value) bool {
	switch p.kind {
	case propNumber:
		n, ok := v.(*syntax.Number)
		if !ok {
			return false
		}
		p.cell.(*Prop[float64]).val = n.Val
		return true
	case propScalar:
		// A value is the sort key, so a number is kept as its exact decimal
		// text: `3.14` sorts and compares as written, and `42` does not become
		// "42.000000".
		sp := p.cell.(*Prop[string])
		switch x := v.(type) {
		case *syntax.String:
			sp.val = x.Val
		case *syntax.Ident:
			sp.val = x.Val
		case *syntax.Number:
			sp.val = strconv.FormatFloat(x.Val, 'f', -1, 64)
		case *syntax.Bool:
			sp.val = strconv.FormatBool(x.Val)
		default:
			return false
		}
		return true
	case propString:
		sp := p.cell.(*Prop[string])
		switch s := v.(type) {
		case *syntax.String:
			sp.val = s.Val
		case *syntax.Ident:
			sp.val = s.Val
		default:
			return false
		}
		return true
	case propEnum:
		var s string
		switch x := v.(type) {
		case *syntax.Ident:
			s = x.Val
		case *syntax.String:
			s = x.Val
		default:
			return false
		}
		if p.allows(s) {
			p.cell.(*Prop[string]).val = s
			return true
		}
		return false
	case propBool:
		b, ok := v.(*syntax.Bool)
		if !ok {
			return false
		}
		p.cell.(*Prop[bool]).val = b.Val
		return true
	case propColor:
		cp := p.cell.(*Prop[uint32])
		switch c := v.(type) {
		case *syntax.Color:
			cp.base.clearColorRole(p.Name)
			cp.base.setGradient(p.Name, nil)
			cp.val = c.RGBA
		case *syntax.String:
			// A gradient is a fill, not a colour; only background paints one.
			if csscolor.IsGradient(c.Val) {
				if p.Name != "background" {
					return false
				}
				g, ok := csscolor.ParseGradient(c.Val)
				if !ok {
					return false
				}
				cp.base.clearColorRole(p.Name)
				cp.base.setGradient(p.Name, g)
				cp.val = 0
				return true
			}
			rgba, ok := csscolor.Parse(c.Val)
			if !ok {
				return false
			}
			cp.base.clearColorRole(p.Name)
			cp.base.setGradient(p.Name, nil)
			cp.val = rgba
		case *syntax.Ident:
			// A CSS colour keyword is a literal, not a theme role.
			if rgba, ok := csscolor.Parse(c.Val); ok {
				cp.base.clearColorRole(p.Name)
				cp.base.setGradient(p.Name, nil)
				cp.val = rgba
				return true
			}
			if !cp.base.setColorRole(p.Name, c.Val) {
				return false
			}
		default:
			return false
		}
		return true
	case propShadow:
		sh, ok := v.(*syntax.Shadow)
		if !ok {
			return false
		}
		sp := p.cell.(*ShadowProp)
		sp.set(shadowLayers(sh))
		sp.goSet = false
		sp.explicit = true
		if sp.hasRole() {
			sp.base.listShadow()
		}
		return true
	}
	return false
}

func (p *NamedProp) allows(s string) bool {
	if p.enum == nil {
		return false
	}
	for _, a := range *p.enum {
		if s == a {
			return true
		}
	}
	return false
}

// CopyFrom copies another node's property of the same name and type. Hot reload
// uses it to carry Go-set values across a reload.
func (p *NamedProp) CopyFrom(src NamedProp) bool {
	switch p.kind {
	case propNumber:
		v, ok := src.Get().(float64)
		if !ok {
			return false
		}
		p.cell.(*Prop[float64]).val = v
	case propString, propEnum:
		v, ok := src.Get().(string)
		if !ok || (p.kind == propEnum && !p.allows(v)) {
			return false
		}
		p.cell.(*Prop[string]).val = v
	case propBool:
		v, ok := src.Get().(bool)
		if !ok {
			return false
		}
		p.cell.(*Prop[bool]).val = v
	case propColor:
		v, ok := src.Get().(uint32)
		if !ok {
			return false
		}
		p.cell.(*Prop[uint32]).val = v
	case propShadow:
		layers, ok := src.Get().([]ShadowLayer)
		if !ok {
			return false
		}
		sp := p.cell.(*ShadowProp)
		sp.set(layers)
		sp.goSet = true
		sp.explicit = true
		if sp.hasRole() {
			sp.base.listShadow()
		}
		return true
	default:
		return false
	}
	p.setGoSet(true)
	return true
}

// SetColor writes a resolved 0xRRGGBBAA without going through a literal. Only
// color properties have it; the theme resolver uses it.
func (p *NamedProp) SetColor(rgba uint32) {
	if p.kind == propColor {
		p.cell.(*Prop[uint32]).val = rgba
	}
}

// GoSet reports whether Go code changed this property since the tree was built.
func (p *NamedProp) GoSet() bool {
	switch p.kind {
	case propNumber:
		return p.cell.(*Prop[float64]).goSet
	case propString, propEnum:
		return p.cell.(*Prop[string]).goSet
	case propBool:
		return p.cell.(*Prop[bool]).goSet
	case propColor:
		return p.cell.(*Prop[uint32]).goSet
	case propShadow:
		return p.cell.(*ShadowProp).goSet
	}
	return false
}

func (p *NamedProp) setGoSet(v bool) {
	switch p.kind {
	case propNumber:
		p.cell.(*Prop[float64]).goSet = v
	case propString, propEnum:
		p.cell.(*Prop[string]).goSet = v
	case propBool:
		p.cell.(*Prop[bool]).goSet = v
	case propColor:
		p.cell.(*Prop[uint32]).goSet = v
	case propShadow:
		p.cell.(*ShadowProp).goSet = v
	}
}

// StringProp builds a NamedProp for a string cell.
func StringProp(b *NodeBase, name string, p *Prop[string]) NamedProp {
	wire(b, name, p)
	return NamedProp{Name: name, kind: propString, cell: p}
}

// ScalarProp builds a NamedProp for a string cell that also accepts a number or
// a bool literal, formatting it as text.
func ScalarProp(b *NodeBase, name string, dirty DirtyKind, p *Prop[string]) NamedProp {
	wire(b, name, p)
	return NamedProp{Name: name, Dirty: dirty, kind: propScalar, cell: p}
}

// NumberProp builds a NamedProp for a float64 cell.
func NumberProp(b *NodeBase, name string, dirty DirtyKind, p *Prop[float64]) NamedProp {
	wire(b, name, p)
	return NamedProp{Name: name, Dirty: dirty, kind: propNumber, cell: p}
}

// BoolProp builds a NamedProp for a bool cell.
func BoolProp(b *NodeBase, name string, dirty DirtyKind, p *Prop[bool]) NamedProp {
	wire(b, name, p)
	return NamedProp{Name: name, Dirty: dirty, kind: propBool, cell: p}
}

// ColorProp builds a NamedProp for a uint32 RGBA cell.
func ColorProp(b *NodeBase, name string, dirty DirtyKind, p *Prop[uint32]) NamedProp {
	wire(b, name, p)
	return NamedProp{Name: name, Dirty: dirty, kind: propColor, cell: p}
}

// NamedShadow builds a NamedProp for the common `shadow` cell.
func NamedShadow(b *NodeBase, name string, p *ShadowProp) NamedProp {
	p.base = b
	return NamedProp{Name: name, Dirty: DirtyPaint, kind: propShadow, cell: p}
}

// EnumProp builds a NamedProp whose literal must be one of allowed.
func EnumProp(b *NodeBase, name string, dirty DirtyKind, p *Prop[string], allowed ...string) NamedProp {
	wire(b, name, p)
	return NamedProp{Name: name, Dirty: dirty, kind: propEnum, cell: p, enum: &allowed}
}

// wire connects a property cell to its owner so Set can mark the tree dirty.
// It runs when the node's table is first built, before any literal is applied.
func wire[T comparable](b *NodeBase, name string, p *Prop[T]) {
	p.base = b
	p.name = name
}
