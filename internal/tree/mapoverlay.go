package tree

import (
	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/schema"
)

// Coord is one [lon, lat] pair, the shape a Path's coordinate list carries.
type Coord = schema.Coord

// Geom is one polygon drawn on a Map: a ring of coordinates in `data`, a stroke
// in `lineColor` and an optional `fillColor`. A design writes one Geom per
// polygon, usually from a loop over an API's geometry.
//
// `data` is a list of `[lon, lat]` pairs, the same coordinate shape a Path
// takes; the ring closes itself.
type Geom struct {
	NodeBase
	Data      ListProp[schema.Coord]
	LineColor Prop[uint32]
	FillColor Prop[uint32]

	ring      []LatLng
	fill      *img.Data
	fillColor uint32
	fillBox   [4]float64
}

// Kind implements Node.
func (*Geom) Kind() string { return "Geom" }

func newGeom(base NodeBase) *Geom {
	g := &Geom{
		NodeBase:  base,
		Data:      ListProp[schema.Coord]{parse: listParse[schema.Coord](schema.TypeCoords)},
		LineColor: newProp(nil, "lineColor", DirtyPaint, uint32(0)),
		FillColor: newProp(nil, "fillColor", DirtyPaint, uint32(0)),
	}
	g.Data.onChange = g.invalidate
	return g
}

func (g *Geom) namedProps() []NamedProp {
	return append(commonProps(&g.NodeBase),
		ListPropOf(&g.NodeBase, "data", DirtyPaint, &g.Data),
		ColorProp(&g.NodeBase, "lineColor", DirtyPaint, &g.LineColor),
		ColorProp(&g.NodeBase, "fillColor", DirtyPaint, &g.FillColor),
	)
}

func (g *Geom) invalidate() {
	g.ring = nil
	g.fill = nil
}

// Ring returns the polygon's closed ring, with the last point joining the first.
func (g *Geom) Ring() []LatLng {
	if g.ring != nil {
		return g.ring
	}
	data := g.Data.Get()
	ring := make([]LatLng, 0, len(data))
	for _, c := range data {
		ring = append(ring, LatLng{Lat: c[1], Lng: c[0]})
	}
	if len(ring) > 0 && ring[0] != ring[len(ring)-1] {
		ring = append(ring, ring[0])
	}
	g.ring = ring
	return ring
}

// Fill returns the rasterized fill and its normalized Mercator box, or nil when
// the polygon has no fill colour. It is cached until the data or colour changes.
func (g *Geom) Fill() (*img.Data, [4]float64) {
	color := g.FillColor.Get()
	if color&0xFF == 0 {
		return nil, [4]float64{}
	}
	ring := g.Ring()
	if len(ring) < 3 {
		return nil, [4]float64{}
	}
	if g.fill != nil && g.fillColor == color {
		return g.fill, g.fillBox
	}
	g.fillColor = color
	g.fill, g.fillBox = fillMask([][][]LatLng{{ring}}, color)
	return g.fill, g.fillBox
}

// Path is an open polyline over a Map. `coordinates` takes one [lon, lat] pair
// per point, the shape a coordinate list carries.
type Path struct {
	NodeBase
	Coordinates ListProp[schema.Coord]
	LineColor   Prop[uint32]
}

// Kind implements Node.
func (*Path) Kind() string { return "Path" }

func newPath(base NodeBase) *Path {
	return &Path{
		NodeBase:    base,
		Coordinates: ListProp[schema.Coord]{parse: listParse[schema.Coord](schema.TypeCoords)},
		LineColor:   newProp(nil, "lineColor", DirtyPaint, uint32(0)),
	}
}

func (p *Path) namedProps() []NamedProp {
	return append(commonProps(&p.NodeBase),
		ListPropOf(&p.NodeBase, "coordinates", DirtyPaint, &p.Coordinates),
		ColorProp(&p.NodeBase, "lineColor", DirtyPaint, &p.LineColor),
	)
}

// Points is the path's coordinates in drawing order.
func (p *Path) Points() []LatLng {
	coords := p.Coordinates.Get()
	out := make([]LatLng, len(coords))
	for i, c := range coords {
		out[i] = LatLng{Lat: c[1], Lng: c[0]}
	}
	return out
}

// Point is a marker on a Map. `coordinate` is `[lon, lat]`, and the children are
// drawn centred on it: a pin icon, a badge, whatever the place needs.
type Point struct {
	NodeBase
	Coordinate ListProp[float64]
}

// Kind implements Node.
func (*Point) Kind() string { return "Point" }

func newPoint(base NodeBase) *Point {
	return &Point{
		NodeBase:   base,
		Coordinate: ListProp[float64]{parse: listParse[float64](schema.TypeNumber)},
	}
}

func (p *Point) namedProps() []NamedProp {
	return append(commonProps(&p.NodeBase),
		ListPropOf(&p.NodeBase, "coordinate", DirtyPaint, &p.Coordinate),
	)
}

// Anchor returns the map-local position of the marker, and false until the
// coordinate has two numbers.
func (p *Point) Anchor(m *Map) (x, y float64, ok bool) {
	c := p.Coordinate.Get()
	if len(c) < 2 {
		return 0, 0, false
	}
	x, y = m.Project(c[1], c[0])
	return x, y, true
}
