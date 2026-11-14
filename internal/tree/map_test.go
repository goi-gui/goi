package tree

import (
	"math"
	"testing"
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
	x, y := m.Project(Point{Lat: 41.0082, Lng: 28.9784})
	if math.Abs(x-200) > 1e-6 || math.Abs(y-150) > 1e-6 {
		t.Fatalf("centre projects to %v,%v, want the middle", x, y)
	}
	for _, p := range []Point{{41, 29}, {-33.9, 151.2}, {0, 0}, {60, -120}} {
		x, y := m.Project(p)
		back := Point{
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
	m.BBox.Set("26,36,45,42")
	m.Prepare(400, 300)
	lat, lng, zoom := m.View()
	if zoom < 4 || zoom > 9 {
		t.Fatalf("zoom = %v for Turkey, want a country-level view", zoom)
	}
	if lat < 36 || lat > 42 || lng < 26 || lng > 45 {
		t.Fatalf("centre %v,%v is outside the box", lat, lng)
	}
	x0, y0 := m.Project(Point{Lat: 42, Lng: 26})
	x1, y1 := m.Project(Point{Lat: 36, Lng: 45})
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

	anchor := Point{Lat: 40, Lng: 28}
	x, y := m.Project(anchor)
	m.ZoomAt(x, y, 1.5)
	bx, by := m.Project(anchor)
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

// TestParseGeoJSON covers the shapes a boundary file uses, including the
// Feature and FeatureCollection wrappers.
func TestParseGeoJSON(t *testing.T) {
	src := `{"type":"FeatureCollection","features":[
		{"type":"Feature","geometry":{"type":"Polygon","coordinates":[
			[[26,36],[45,36],[45,42],[26,42],[26,36]],
			[[29,39],[31,39],[31,41],[29,41],[29,39]]
		]}},
		{"type":"Feature","geometry":{"type":"LineString","coordinates":[[26,36],[45,42]]}},
		{"type":"Feature","geometry":{"type":"Point","coordinates":[29,41]}}
	]}`
	g, err := ParseGeoJSON(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Polys) != 1 || len(g.Polys[0]) != 2 || len(g.Polys[0][0]) != 5 {
		t.Fatalf("polygons = %d, want one with an outer ring and a hole", len(g.Polys))
	}
	// GeoJSON writes longitude first; the point keeps latitude in Lat.
	if got := g.Polys[0][0][0]; got != (Point{Lat: 36, Lng: 26}) {
		t.Fatalf("first vertex = %v, want lat 36 lon 26", got)
	}
	if len(g.Lines) != 1 || len(g.Lines[0]) != 2 {
		t.Fatalf("lines = %d", len(g.Lines))
	}
	if len(g.Points) != 1 || g.Points[0] != (Point{Lat: 41, Lng: 29}) {
		t.Fatalf("points = %v", g.Points)
	}
	if _, err := ParseGeoJSON(`{"type":"Nope"}`); err == nil {
		t.Fatal("an unsupported geometry type parsed")
	}
}

// TestMapGeomReparsesOnChange checks that the geometry is cached until the text
// changes, which is what keeps a per-frame paint from parsing JSON.
func TestMapGeomReparsesOnChange(t *testing.T) {
	m := newMap()
	m.Geom.Set(`{"type":"Point","coordinates":[29,41]}`)
	if g := m.Geometry(); len(g.Points) != 1 {
		t.Fatalf("points = %d", len(g.Points))
	}
	first := m.Geometry()
	if first != m.Geometry() {
		t.Fatal("the geometry was parsed again without a change")
	}
	m.Geom.Set(`{"type":"Point","coordinates":[30,42]}`)
	if g := m.Geometry(); g.Points[0].Lng != 30 {
		t.Fatalf("stale geometry: %v", g.Points)
	}
}
