package schema

import (
	"fmt"
	"math"

	"github.com/goi-gui/goi/internal/csscolor"
	"github.com/goi-gui/goi/internal/syntax"
)

// Coord is one position of a coordinate list, as [lon, lat]. GeoJSON order is
// the one every place API writes, and the type keeps it apart from a
// latitude-first display string.
type Coord [2]float64

// Lon returns the longitude.
func (c Coord) Lon() float64 { return c[0] }

// Lat returns the latitude.
func (c Coord) Lat() float64 { return c[1] }

// ListColor is one colour of a colour list: a literal, or the theme role it
// names, which the painter resolves against the scheme it draws with. A role is
// written with its namespace, `theme.primary`, the way every other colour
// reference is.
type ListColor struct {
	Color uint32
	Role  string
	// Alpha is the opacity of a role, 0-255; HasAlpha tells 0 apart from the
	// opaque default. A literal Color already carries its own alpha.
	Alpha    uint8
	HasAlpha bool
}

// ListItem reads one item of a list property. elem is the property's Elem type,
// so the checker and the builder read a list the same way.
func ListItem(elem ValueType, v syntax.Value) (any, error) {
	switch elem {
	case TypeNumber:
		if n, ok := v.(*syntax.Number); ok {
			return n.Val, nil
		}
		return nil, fmt.Errorf("expected a number")
	case TypeCoords:
		list, ok := v.(*syntax.List)
		if !ok || len(list.Items) != 2 {
			return nil, fmt.Errorf("expected [lon, lat]")
		}
		lon, ok1 := list.Items[0].(*syntax.Number)
		lat, ok2 := list.Items[1].(*syntax.Number)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("expected [lon, lat]")
		}
		return Coord{lon.Val, lat.Val}, nil
	case TypeColor:
		switch c := v.(type) {
		case *syntax.Color:
			return ListColor{Color: c.RGBA, Alpha: 255}, nil
		case *syntax.Ref:
			// Only a namespaced role names a colour; a bare word would be a
			// CSS colour name, which the Ident case below takes. A theme role
			// is kept by name; a custom colour keeps its prefix, because the
			// painter cannot resolve one.
			lc := ListColor{}
			lc.HasAlpha = c.HasAlpha
			if c.HasAlpha {
				lc.Alpha = uint8(math.Round(c.Alpha * 255 / 100))
			}
			switch c.ID {
			case "theme":
				lc.Role = c.Prop
				return lc, nil
			case "customColor":
				// The resolver's namespace, so the colour pass can ask the
				// same lookup a colour property does.
				lc.Role = "custom:" + c.Prop
				return lc, nil
			}
			return nil, fmt.Errorf("%s.%s is not a colour", c.ID, c.Prop)
		case *syntax.String:
			rgba, ok := csscolor.ParseWithOpacity(c.Val)
			if !ok {
				return nil, fmt.Errorf("%q is not a colour", c.Val)
			}
			return ListColor{Color: rgba, Alpha: 255}, nil
		case *syntax.Ident:
			// A CSS colour name, which is a literal; a role needs its
			// namespace so the list reads like every other colour property.
			rgba, ok := csscolor.ParseWithOpacity(c.Val)
			if !ok {
				return nil, fmt.Errorf("%q is not a colour; a theme role is written theme.%s", c.Val, c.Val)
			}
			return ListColor{Color: rgba, Alpha: 255}, nil
		}
		return nil, fmt.Errorf("expected a colour or a theme role")
	default:
		if txt, ok := literalText(v); ok {
			return txt, nil
		}
		return nil, fmt.Errorf("expected a string")
	}
}

// isColorRefValue reports whether a value names a colour role, which a colour
// list keeps as a role and no other list accepts.
func isColorRefValue(v syntax.Value) bool {
	r, ok := v.(*syntax.Ref)
	return ok && (r.ID == "theme" || r.ID == "customColor")
}

// dynamicValue reports whether a list item is an expression: its type is only
// known once it runs, so the checker cannot judge the item itself.
func dynamicValue(v syntax.Value) bool {
	switch v.(type) {
	case *syntax.Ref, *syntax.Index, *syntax.Call, *syntax.Binary, *syntax.Unary, *syntax.Ternary:
		return true
	}
	return false
}
