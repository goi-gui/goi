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

// writeRole stores a colour property as a theme or custom role, so the resolver
// rewrites it whenever the scheme changes. This is the expression layer's
// counterpart to writing `theme.primary` as a literal: a conditional can pick a
// role and the property keeps following the theme. A repeat of the role already
// on the cell costs nothing, which is what makes the per-frame pass cheap.
func (b *NodeBase) writeRole(name, role string) {
	b.properties()
	i, ok := b.byName[name]
	if !ok || b.props[i].kind != propColor || b.colorRoleFor(name) == role {
		return
	}
	b.setGradient(name, nil)
	if b.setColorRole(name, role) {
		b.mark(DirtyPaint)
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
	for _, b := range t.shadowNodes {
		if b.resolveShadows(lookup) {
			b.mark(DirtyPaint)
			changed = true
		}
	}
	// A chart's series colours are a list, so they have no cell the loop above
	// could write. A role in one is resolved into the series it belongs to,
	// which is what makes a custom colour follow SetCustomColor like the rest.
	for _, c := range t.roleCharts {
		if c.resolveListRoles(lookup) {
			c.Base().mark(DirtyPaint)
			changed = true
		}
	}
	return changed
}
