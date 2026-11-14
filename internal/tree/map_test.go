package tree

import (
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/schema"
)

func testMap(center string, zoom float64, w, h float64) *Map {
	m := newMap()
	m.Center.Set(center)
	m.Zoom.Set(zoom)
	m.Prepare(w, h)
	return m
}

// TestMapProjectionsRoundTrip checks the Web Mercator pair: a position
// projected and unprojected lands back where it started, and the camera's own
// centre is the middle of the map.
func TestMapProjectionsRoundTrip(t *testing.T) {
	m := testMap("41.0082,28.9784", 11, 400, 300)
	x, y := m.Project(41.0082, 28.9784)
	if math.Abs(x-200) > 1e-6 || math.Abs(y-150) > 1e-6 {
		t.Fatalf("centre projects to %v,%v, want the middle", x, y)
	}
	for _, p := range []LatLng{{41, 29}, {-33.9, 151.2}, {0, 0}, {60, -120}} {
		x, y := m.Project(p.Lat, p.Lng)
		back := LatLng{
			Lat: invMercatorY(mercatorY(m.lat) + (y-m.h/2)/(mapTileSize*math.Pow(2, m.zoom))),
			Lng: invMercatorX(mercatorX(m.lng) + (x-m.w/2)/(mapTileSize*math.Pow(2, m.zoom))),
		}
		if math.Abs(back.Lat-p.Lat) > 1e-6 || math.Abs(back.Lng-p.Lng) > 1e-6 {
			t.Fatalf("%v round-trips to %v", p, back)
		}
	}
}

// TestMapBBoxFitsTheView checks that a bbox covers the whole box it names and
// has no more room than that.
func TestMapBBoxFitsTheView(t *testing.T) {
	m := newMap()
	m.BBox.Set([]float64{26, 36, 45, 42})
	m.Prepare(400, 300)
	lat, lng, zoom := m.View()
	if zoom < 4 || zoom > 9 {
		t.Fatalf("zoom = %v for Turkey, want a country-level view", zoom)
	}
	if lat < 36 || lat > 42 || lng < 26 || lng > 45 {
		t.Fatalf("centre %v,%v is outside the box", lat, lng)
	}
	x0, y0 := m.Project(42, 26)
	x1, y1 := m.Project(36, 45)
	if x0 < -0.5 || y0 < -0.5 || x1 > 400.5 || y1 > 300.5 {
		t.Fatalf("box corners at %v,%v..%v,%v leave the map", x0, y0, x1, y1)
	}
	// A resize refits rather than leaving the box off-centre.
	m.Prepare(800, 300)
	if _, _, z := m.View(); z <= zoom {
		t.Fatalf("zoom = %v after a wider map, want more than %v", z, zoom)
	}
}

// TestMapTilesCoverTheView checks the visible tile set for a normal view, and
// that longitudes across the antimeridian wrap into the tile grid.
func TestMapTilesCoverTheView(t *testing.T) {
	m := testMap("41,29", 4, 400, 300)
	tiles := m.ViewTiles()
	if len(tiles) == 0 {
		t.Fatal("no tiles")
	}
	for _, tl := range tiles {
		if tl.Z != 4 || tl.X < 0 || tl.X >= 16 || tl.Y < 0 || tl.Y >= 16 {
			t.Fatalf("tile %+v outside zoom 4", tl)
		}
		if tl.W != 256 || tl.H != 256 {
			t.Fatalf("tile size %vx%v, want 256x256", tl.W, tl.H)
		}
	}

	across := testMap("0,179.9", 3, 900, 300)
	seen := map[int]bool{}
	for _, tl := range across.ViewTiles() {
		if tl.X < 0 || tl.X >= 8 {
			t.Fatalf("x %d did not wrap", tl.X)
		}
		seen[tl.X] = true
	}
	if !seen[0] || !seen[7] {
		t.Fatalf("a view across the antimeridian should show both edges: %v", seen)
	}
}

// TestMapPanAndZoom checks the two gestures: a drag moves the camera by the
// pixels it travelled, and a wheel keeps the point under the pointer fixed.
func TestMapPanAndZoom(t *testing.T) {
	m := testMap("41,29", 5, 400, 300)
	_, lng0, zoom0 := m.View()
	m.PanBy(100, 0)
	_, lng1, zoom1 := m.View()
	if lng1 >= lng0 {
		t.Fatalf("dragging right moved the view east: %v -> %v", lng0, lng1)
	}
	if zoom1 != zoom0 {
		t.Fatalf("a pan changed the zoom: %v -> %v", zoom0, zoom1)
	}

	anchor := LatLng{Lat: 40, Lng: 28}
	x, y := m.Project(anchor.Lat, anchor.Lng)
	m.ZoomAt(x, y, 1.5)
	bx, by := m.Project(anchor.Lat, anchor.Lng)
	if math.Abs(bx-x) > 0.01 || math.Abs(by-y) > 0.01 {
		t.Fatalf("anchor moved from %v,%v to %v,%v while zooming", x, y, bx, by)
	}
	if _, _, z := m.View(); math.Abs(z-(zoom0+1.5)) > 1e-9 {
		t.Fatalf("zoom = %v, want %v", z, zoom0+1.5)
	}
	// The property follows the gesture, so a design or an expression sees it.
	if m.Zoom.Get() != zoom0+1.5 {
		t.Fatalf("zoom property = %v, want %v", m.Zoom.Get(), zoom0+1.5)
	}
}

// TestMapZoomClamps keeps the wheel from running past the tile services' range.
func TestMapZoomClamps(t *testing.T) {
	m := testMap("0,0", 0, 400, 300)
	m.ZoomAt(200, 150, -5)
	if _, _, z := m.View(); z != 0 {
		t.Fatalf("zoom = %v, want the 0 floor", z)
	}
	m.ZoomAt(200, 150, 40)
	if _, _, z := m.View(); z != mapMaxZoom {
		t.Fatalf("zoom = %v, want the %v ceiling", z, mapMaxZoom)
	}
}

// TestGeomRingAndFill covers the Geom component: data is flat [lon, lat], the
// ring closes itself, and a fill colour rasterizes the inside while a hole-free
// polygon keeps its corners clear.
func TestGeomRingAndFill(t *testing.T) {
	g := newGeom(NodeBase{})
	g.Data.Set([]float64{28, 40, 30, 40, 30, 42, 28, 42})
	ring := g.Ring()
	if len(ring) != 5 {
		t.Fatalf("ring = %d points, want the four corners plus the closing point", len(ring))
	}
	if ring[0] != (LatLng{Lat: 40, Lng: 28}) || ring[4] != ring[0] {
		t.Fatalf("ring = %v", ring)
	}
	if d, _ := g.Fill(); d != nil {
		t.Fatal("a Geom with no fill colour produced a mask")
	}
	g.FillColor.Set(0x11223344)
	d, box := g.Fill()
	if d == nil || d.W <= 0 || d.H <= 0 {
		t.Fatal("no fill mask")
	}
	if !(box[0] < box[2] && box[1] < box[3]) {
		t.Fatalf("fill box = %v", box)
	}
	center := ((d.H/2)*d.W + d.W/2) * 4
	if d.Pix[center+3] != 0x44 {
		t.Fatalf("centre alpha = %#x, want the fill's 0x44", d.Pix[center+3])
	}
	// Changing the data rebuilds both the ring and the mask.
	g.Data.Set([]float64{28, 40, 30, 40, 30, 42})
	if len(g.Ring()) != 4 {
		t.Fatalf("a changed ring kept its old cache: %v", g.Ring())
	}
}

// TestPointAndPathCoordinates checks the two coordinate shapes: a Point takes
// [lon, lat] and anchors at the map's projected pixel, a Path takes pairs.
func TestPointAndPathCoordinates(t *testing.T) {
	m := testMap("41,29", 5, 400, 300)
	p := newPoint(NodeBase{})
	if _, _, ok := p.Anchor(m); ok {
		t.Fatal("an empty coordinate anchored")
	}
	p.Coordinate.Set([]float64{28, 40})
	x, y, ok := p.Anchor(m)
	if !ok {
		t.Fatal("a two-number coordinate did not anchor")
	}
	wx, wy := m.Project(40, 28)
	if x != wx || y != wy {
		t.Fatalf("anchor = %v,%v, want %v,%v", x, y, wx, wy)
	}

	path := newPath(NodeBase{})
	path.Coordinates.Set([]schema.Coord{{28, 40}, {29, 41}})
	pts := path.Points()
	if len(pts) != 2 || pts[0] != (LatLng{Lat: 40, Lng: 28}) || pts[1] != (LatLng{Lat: 41, Lng: 29}) {
		t.Fatalf("path points = %v", pts)
	}
}

// TestListPropertiesReadBridgeSlices checks the shape an API demo needs: a
// Geom's data and a Point's coordinate are filled from a bridge slice, not a
// literal, so the loop can build one polygon per API row.
func TestListPropertiesReadBridgeSlices(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge {
		Data   []float64
		Marker []float64
	}
	Window {
		Map {
			width: 200 height: 200
			Geom { id: g data: bridge.Data }
			Point { id: p coordinate: bridge.Marker }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetData("Data", []float64{28, 40, 30, 40, 30, 42})
	tr.SetData("Marker", []float64{29, 41})
	tr.ApplyExpressions()

	g, _ := tr.ByID("g")
	geom := g.(*Geom)
	if ring := geom.Ring(); len(ring) != 4 || ring[0] != (LatLng{Lat: 40, Lng: 28}) {
		t.Fatalf("geom ring = %v", ring)
	}
	p, _ := tr.ByID("p")
	point := p.(*Point)
	if c := point.Coordinate.Get(); len(c) != 2 || c[0] != 29 || c[1] != 41 {
		t.Fatalf("point coordinate = %v", c)
	}
}
