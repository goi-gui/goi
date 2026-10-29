package tree

// colorSpec is one property written as a named theme role instead of a literal.
// The name stays until the theme changes, then ResolveColors rewrites the cell.
type colorSpec struct {
	name string
	role string
}

func (b *NodeBase) setColorRole(name, role string) bool {
	b.properties()
	if _, ok := b.byName[name]; !ok {
		return false
	}
	for i := range b.colors {
		if b.colors[i].name == name {
			b.colors[i].role = role
			return true
		}
	}
	b.colors = append(b.colors, colorSpec{name: name, role: role})
	if b.tree != nil && !b.colorListed {
		b.colorListed = true
		b.tree.colorNodes = append(b.tree.colorNodes, b)
	}
	return true
}

func (b *NodeBase) colorRoleFor(name string) string {
	for i := range b.colors {
		if b.colors[i].name == name {
			return b.colors[i].role
		}
	}
	return ""
}

func (b *NodeBase) clearColorRole(name string) {
	for i := range b.colors {
		if b.colors[i].name == name {
			b.colors = append(b.colors[:i], b.colors[i+1:]...)
			return
		}
	}
}

// writeColor stores a resolved color without marking the node dirty. The
// resolver decides whether the change needs a repaint.
func (b *NodeBase) writeColor(name string, rgba uint32) bool {
	b.properties()
	if i, ok := b.byName[name]; ok {
		b.props[i].SetColor(rgba)
		return true
	}
	return false
}

// ResolveColors rewrites every role-valued color from the current scheme. It
// returns true when at least one cell changed, which marks those nodes for
// repaint. The render loop calls it once per tick; nodes without roles cost
// nothing.
func (t *Tree) ResolveColors(lookup func(string) (uint32, bool)) bool {
	changed := false
	for _, b := range t.colorNodes {
		for _, spec := range b.colors {
			rgba, ok := lookup(spec.role)
			if !ok {
				continue
			}
			if cur, ok := b.PropValue(spec.name); ok {
				if old, ok := cur.(uint32); ok && old == rgba {
					continue
				}
			}
			if b.writeColor(spec.name, rgba) {
				b.mark(b.PropDirty(spec.name))
				changed = true
			}
		}
	}
	return changed
}
