package tree

import "github.com/goi-gui/goi/internal/syntax"

// ShadowLayer is one shadow the renderer can draw. A layer with Inset draws
// inside the shape, the way CSS box-shadow does; every other layer draws behind
// it.
//
// The numbers are logical pixels: X and Y are the offset, Blur the softness and
// Spread the growth of the shape. Offset and blur apply to text too, where the
// shape is the glyph itself.
type ShadowLayer struct {
	X, Y   float64
	Blur   float64
	Spread float64
	Color  uint32 // 0xRRGGBBAA
	Inset  bool
	// Role is the theme role the design named instead of a literal. It is kept
	// so a theme switch can resolve the layer again; Color is what paint reads.
	Role string
}

// ShadowProp is the `shadow` cell. The layers are not a comparable value, so it
// is not a Prop[[]ShadowLayer]; it carries the same observable behaviour through
// a NamedProp.
type ShadowProp struct {
	layers []ShadowLayer
	goSet  bool
	// explicit is true once the design or Go set the property, which is what
	// lets an explicit `shadow: none` replace a component's M3 elevation
	// instead of being indistinguishable from "not written".
	explicit bool
}

// set replaces the layers without marking the node: callers do that.
func (p *ShadowProp) set(layers []ShadowLayer) {
	p.layers = append([]ShadowLayer(nil), layers...)
}

func (p *ShadowProp) hasRole() bool {
	for _, l := range p.layers {
		if l.Role != "" {
			return true
		}
	}
	return false
}

// shadowProp builds the NamedProp for the common `shadow` property.
func shadowProp(b *NodeBase, name string, p *ShadowProp) NamedProp {
	return NamedProp{
		Name:  name,
		Dirty: DirtyPaint,
		Get: func() any {
			out := make([]ShadowLayer, len(p.layers))
			copy(out, p.layers)
			return out
		},
		SetLiteral: func(v syntax.Value) bool {
			sh, ok := v.(*syntax.Shadow)
			if !ok {
				return false
			}
			p.set(shadowLayers(sh))
			p.goSet = false
			p.explicit = true
			if p.hasRole() {
				b.listShadow()
			}
			return true
		},
		CopyFrom: func(src NamedProp) bool {
			layers, ok := src.Get().([]ShadowLayer)
			if !ok {
				return false
			}
			p.set(layers)
			p.goSet = true
			p.explicit = true
			if p.hasRole() {
				b.listShadow()
			}
			return true
		},
		goSet: func() bool { return p.goSet },
	}
}

// shadowLayers converts the parsed literal into layers paint can use. A colour
// role stays a name until ResolveColors fills it in; a hex literal is final.
func shadowLayers(sh *syntax.Shadow) []ShadowLayer {
	out := make([]ShadowLayer, len(sh.Layers))
	for i, l := range sh.Layers {
		out[i] = ShadowLayer{X: l.X, Y: l.Y, Blur: l.Blur, Spread: l.Spread, Inset: l.Inset}
		switch c := l.Color.(type) {
		case *syntax.Color:
			out[i].Color = c.RGBA
		case *syntax.Ident:
			out[i].Role = c.Val
		}
	}
	return out
}

// listShadow registers the node for shadow-colour resolution.
func (b *NodeBase) listShadow() {
	if b.tree == nil || b.shadowListed {
		return
	}
	b.shadowListed = true
	b.tree.shadowNodes = append(b.tree.shadowNodes, b)
}

// resolveShadows rewrites every role-coloured shadow layer from the current
// scheme and reports whether a layer changed. The role stays on the layer so a
// later theme switch resolves it again.
func (b *NodeBase) resolveShadows(lookup func(string) (uint32, bool)) bool {
	changed := false
	for i := range b.shadow.layers {
		role := b.shadow.layers[i].Role
		if role == "" {
			continue
		}
		rgba, ok := lookup(role)
		if !ok || b.shadow.layers[i].Color == rgba {
			continue
		}
		b.shadow.layers[i].Color = rgba
		changed = true
	}
	return changed
}
