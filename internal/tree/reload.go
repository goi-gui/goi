package tree

import "github.com/ksckaan1/goui/internal/syntax"

// LiteralEqual reports whether two parsed literals carry the same value. Used
// by hot reload to decide if the .ui file changed a property.
func LiteralEqual(a, b syntax.Value) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind() != b.Kind() {
		return false
	}
	switch x := a.(type) {
	case *syntax.String:
		return x.Val == b.(*syntax.String).Val
	case *syntax.Number:
		return x.Val == b.(*syntax.Number).Val
	case *syntax.Bool:
		return x.Val == b.(*syntax.Bool).Val
	case *syntax.Color:
		return x.RGBA == b.(*syntax.Color).RGBA
	case *syntax.Ident:
		return x.Val == b.(*syntax.Ident).Val
	case *syntax.Percent:
		return x.Val == b.(*syntax.Percent).Val
	case *syntax.Ref:
		y := b.(*syntax.Ref)
		return x.ID == y.ID && x.Prop == y.Prop
	case *syntax.Binary:
		y := b.(*syntax.Binary)
		return x.Op == y.Op && LiteralEqual(x.Left, y.Left) && LiteralEqual(x.Right, y.Right)
	case *syntax.Map:
		y := b.(*syntax.Map)
		if len(x.Entries) != len(y.Entries) {
			return false
		}
		for _, xe := range x.Entries {
			found := false
			for _, ye := range y.Entries {
				if xe.Key == ye.Key && LiteralEqual(xe.Value, ye.Value) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return false
}

// literalOf returns the literal an element's AST declared for a property, or nil.
func literalOf(el *syntax.Element, name string) syntax.Value {
	if el == nil {
		return nil
	}
	if p := el.Prop(name); p != nil {
		return p.Value
	}
	return nil
}

// GoSet reports whether a property was last changed by Go code rather than by
// the .ui file.
func (b *NodeBase) GoSet(name string) bool {
	b.properties()
	if i, ok := b.byName[name]; ok {
		return b.props[i].GoSet()
	}
	return false
}

// PropertyNames lists the .ui names of every property the node exposes.
func (b *NodeBase) PropertyNames() []string {
	b.properties()
	out := make([]string, len(b.props))
	for i := range b.props {
		out[i] = b.props[i].Name
	}
	return out
}

// RuntimeValue returns the current value of a property by .ui name.
func (b *NodeBase) RuntimeValue(name string) (any, bool) { return b.PropValue(name) }

// CopyProp copies the current value of a property from src, which must be a
// node of the same type. It is the hot reload carry-over path, so it does not
// mark anything dirty: the caller rebuilds and repaints everything anyway.
func (b *NodeBase) CopyProp(name string, src *NodeBase) bool {
	b.properties()
	src.properties()
	i, ok := b.byName[name]
	if !ok {
		return false
	}
	return b.props[i].CopyFrom(src.props[i])
}

// LiteralChanged reports whether the .ui file changed a property between the
// element that produced src and the element that produced b.
func LiteralChanged(b *NodeBase, name string, src *NodeBase) bool {
	return !LiteralEqual(literalOf(b.AST, name), literalOf(src.AST, name))
}
