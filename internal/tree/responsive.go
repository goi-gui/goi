package tree

import (
	"sort"

	"github.com/goi-gui/goi/internal/csscolor"
	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/syntax"
)

// responsiveProp is one property whose value follows the window width. The
// tiers are sorted by minimum width; fallback is the value the property had
// before the map was applied, used below the first tier.
type responsiveProp struct {
	prop     NamedProp
	tiers    []responsiveTier
	fallback syntax.Value
	// applied is the value last written, so a breakpoint that does not change
	// is a no-op even for expressions.
	applied syntax.Value
}

type responsiveTier struct {
	min   float64
	value syntax.Value
}

// setResponsive registers a map literal for one property. It returns false when
// the property is unknown, a tier name is unknown, or the map is empty.
func (b *NodeBase) setResponsive(name string, m *syntax.Map) bool {
	b.properties()
	i, ok := b.byName[name]
	if !ok {
		return false
	}
	rp := responsiveProp{prop: b.props[i], fallback: valueLiteral(b.props[i].Get())}
	rp.applied = rp.fallback
	for _, e := range m.Entries {
		min, ok := schema.BreakpointMin(e.Key)
		if !ok {
			return false
		}
		// Base is the value for a window narrower than sm, which is also the
		// only value the tree can assume before it knows the real width.
		if e.Key == schema.BaseBreakpoint {
			b.applyValue(name, e.Value)
			rp.applied = e.Value
		}
		rp.tiers = append(rp.tiers, responsiveTier{min: min, value: e.Value})
	}
	if len(rp.tiers) == 0 {
		return false
	}
	sort.SliceStable(rp.tiers, func(i, j int) bool { return rp.tiers[i].min < rp.tiers[j].min })
	b.responsive = append(b.responsive, rp)
	return true
}

// applyBreakpoints writes the value of the largest tier whose threshold is at
// or below width, and marks the node dirty only when the value changed.
func (b *NodeBase) applyBreakpoints(width float64) {
	for i := range b.responsive {
		rp := &b.responsive[i]
		v := rp.fallback
		for _, t := range rp.tiers {
			if t.min <= width {
				v = t.value
			}
		}
		if v == nil {
			continue
		}
		if rp.applied != nil && LiteralEqual(rp.applied, v) {
			// An expression is re-resolved each layout, so only a plain value
			// needs the Go-set check to stay file-owned.
			if isExpression(v) || literalMatches(rp.prop.Get(), v) {
				continue
			}
		}
		if b.applyValue(rp.prop.Name, v) {
			rp.applied = v
			b.mark(rp.prop.Dirty)
		}
	}
}

// ApplyBreakpoints rewrites every responsive property in the tree for the given
// logical window width. The driver calls it once per frame before layout.
func (t *Tree) ApplyBreakpoints(width float64) {
	for _, b := range t.responsive {
		b.applyBreakpoints(width)
	}
}

// valueLiteral converts a typed property value back to a literal, so the value
// a node had before its map can be restored below the first breakpoint.
func valueLiteral(v any) syntax.Value {
	switch x := v.(type) {
	case string:
		return &syntax.Ident{Val: x}
	case float64:
		return &syntax.Number{Val: x}
	case bool:
		return &syntax.Bool{Val: x}
	case uint32:
		return &syntax.Color{RGBA: x}
	}
	return nil
}

// literalMatches reports whether v is already the property's value, mirroring
// what the NamedProp constructors accept.
func literalMatches(cur any, v syntax.Value) bool {
	switch x := v.(type) {
	case *syntax.Number:
		c, ok := cur.(float64)
		return ok && c == x.Val
	case *syntax.Bool:
		c, ok := cur.(bool)
		return ok && c == x.Val
	case *syntax.Color:
		c, ok := cur.(uint32)
		return ok && c == x.RGBA
	case *syntax.Ident:
		c, ok := cur.(string)
		return ok && c == x.Val
	case *syntax.String:
		if c, ok := cur.(string); ok {
			return c == x.Val
		}
		if c, ok := cur.(uint32); ok {
			rgba, ok := csscolor.Parse(x.Val)
			return ok && rgba == c
		}
	}
	return false
}
