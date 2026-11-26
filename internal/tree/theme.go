package tree

// colorSpec is one property written as a named theme role instead of a literal.
// The name stays until the theme changes, then ResolveColors rewrites the cell.
type colorSpec struct {
	name  string
	role  string
	alpha uint8 // 255 is opaque; `/ 50` stores 128
}

func (b *NodeBase) setColorRole(name, role string) bool {
	return b.setColorRoleAlpha(name, role, 255)
}

// setColorRoleAlpha is setColorRole with the opacity of a `/ 0-100` suffix.
func (b *NodeBase) setColorRoleAlpha(name, role string, alpha uint8) bool {
	b.properties()
	if _, ok := b.byName[name]; !ok {
		return false
	}
	for i := range b.colors {
		if b.colors[i].name == name {
			b.colors[i].role = role
			b.colors[i].alpha = alpha
			return true
		}
	}
	b.colors = append(b.colors, colorSpec{name: name, role: role, alpha: alpha})
	if b.tree != nil && !b.colorListed {
		b.colorListed = true
		b.tree.colorNodes = append(b.tree.colorNodes, b)
	}
	return true
}

// colorAlphaFor returns the opacity stored with a role, and whether it is
// explicitly not opaque.
func (b *NodeBase) colorAlphaFor(name string) (uint8, bool) {
	for i := range b.colors {
		if b.colors[i].name == name {
			return b.colors[i].alpha, b.colors[i].alpha != 255
		}
	}
	return 255, false
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

// writeColor stores a resolved color. A transition on the property turns the
// write into its target instead of changing the cell immediately.
func (b *NodeBase) writeColor(name string, rgba uint32) bool {
	if b.transitionColor(name, rgba) {
		return true
	}
	return b.writeColorRaw(name, rgba)
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
			if spec.alpha != 255 {
				rgba = withAlphaByte(rgba, spec.alpha)
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
	// A gradient keeps its roles on the stops, so a theme switch or a changed
	// custom colour recolours every stop that follows one.
	for _, b := range t.gradientNodes {
		if b.resolveGradients(lookup) {
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
