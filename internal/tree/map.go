package tree

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/img"
)

// Point is one WGS84 position, in degrees. GeoJSON writes longitude first,
// while every map URL and every pair people copy writes latitude first; the
// field names are what keeps the two apart.
type Point struct {
	Lat, Lng float64
}

// MapGeometry is what a Map's `geom` draws: polygon outlines, open lines and
// single points. A polygon's outer ring and its holes are kept together, so a
// lake inside a region stays a hole in the outline.
type MapGeometry struct {
	Polys  [][][]Point
	Lines  [][]Point
	Points []Point
}

// Empty reports whether there is nothing to draw.
func (g *MapGeometry) Empty() bool {
	return g == nil || (len(g.Polys) == 0 && len(g.Lines) == 0 && len(g.Points) == 0)
}

// ParseGeoJSON reads a GeoJSON geometry, Feature, FeatureCollection or
// GeometryCollection. Coordinates are in GeoJSON order: longitude, latitude.
func ParseGeoJSON(s string) (*MapGeometry, error) {
	var root geoJSON
	if err := json.Unmarshal([]byte(s), &root); err != nil {
		return nil, fmt.Errorf("map: geom: %w", err)
	}
	if root.Type == "" {
		return nil, fmt.Errorf("map: geom: no GeoJSON type")
	}
	g := &MapGeometry{}
	if err := addGeoJSON(&root, g, 0); err != nil {
		return nil, err
	}
	return g, nil
}

// geoJSON is the subset of the format a boundary needs. Coordinates stay raw
// until the type says how deep the nesting goes.
type geoJSON struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
	Geometry    json.RawMessage `json:"geometry"`
	Geometries  []geoJSON       `json:"geometries"`
	Features    []geoJSON       `json:"features"`
}

func addGeoJSON(n *geoJSON, g *MapGeometry, depth int) error {
	if depth > 8 {
		return fmt.Errorf("map: geom: nested too deep")
	}
	switch n.Type {
	case "Feature":
		if len(n.Geometry) == 0 || string(n.Geometry) == "null" {
			return nil
		}
		var f geoJSON
		if err := json.Unmarshal(n.Geometry, &f); err != nil {
			return fmt.Errorf("map: geom: %w", err)
		}
		return addGeoJSON(&f, g, depth+1)
	case "FeatureCollection":
		for i := range n.Features {
			if err := addGeoJSON(&n.Features[i], g, depth+1); err != nil {
				return err
			}
		}
		return nil
	case "GeometryCollection":
		for i := range n.Geometries {
			if err := addGeoJSON(&n.Geometries[i], g, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	var v any
	if err := json.Unmarshal(n.Coordinates, &v); err != nil {
		return fmt.Errorf("map: geom: %s coordinates: %w", n.Type, err)
	}
	switch n.Type {
	case "Point":
		if p, ok := asPoint(v); ok {
			g.Points = append(g.Points, p)
		}
	case "MultiPoint":
		g.Points = append(g.Points, asRing(v)...)
	case "LineString":
		g.Lines = append(g.Lines, asRing(v))
	case "MultiLineString":
		g.Lines = append(g.Lines, asRings(v)...)
	case "Polygon":
		g.Polys = append(g.Polys, asRings(v))
	case "MultiPolygon":
		g.Polys = append(g.Polys, asPolys(v)...)
	default:
		return fmt.Errorf("map: geom: unsupported type %q", n.Type)
	}
	return nil
}

func asPoint(v any) (Point, bool) {
	a, ok := v.([]any)
	if !ok || len(a) < 2 {
		return Point{}, false
	}
	lon, ok1 := a[0].(float64)
	lat, ok2 := a[1].(float64)
	return Point{Lat: lat, Lng: lon}, ok1 && ok2
}

func asRing(v any) []Point {
	a, _ := v.([]any)
	out := make([]Point, 0, len(a))
	for _, e := range a {
		if p, ok := asPoint(e); ok {
			out = append(out, p)
		}
	}
	return out
}

func asRings(v any) [][]Point {
	a, _ := v.([]any)
	out := make([][]Point, 0, len(a))
	for _, e := range a {
		out = append(out, asRing(e))
	}
	return out
}

func asPolys(v any) [][][]Point {
	a, _ := v.([]any)
	out := make([][][]Point, 0, len(a))
	for _, e := range a {
		out = append(out, asRings(e))
	}
	return out
}

// MapTile is one tile in the slippy-map scheme: a zoom level and an x/y index
// inside it.
type MapTile struct{ Z, X, Y int }

// MapTileView is a tile together with the rectangle it covers inside the map,
// in the map's own pixels.
type MapTileView struct {
	MapTile
	Left, Top, W, H float64
}

// Map draws a slippy map from tiles and can draw GeoJSON boundaries over it. By
// default the tiles come from OpenStreetMap; `tiles` takes any `{z}/{x}/{y}`
// template.
//
// The camera lives in two properties, `center` and `zoom`, which the pointer
// writes as the user pans and zooms. `bbox` overrides both while it is set: the
// map fits that box whenever the box or the map's size changes.
type Map struct {
	Component

	// Tiles is the URL template, with {z}, {x}, {y} and optionally {s} for a
	// subdomain letter.
	Tiles Prop[string]
	// Attribution is the credit line drawn in the corner; empty hides it.
	Attribution Prop[string]
	// Center is `lat,lon`; Zoom is the Web Mercator zoom level, 0..19. Both
	// follow the pointer.
	Center Prop[string]
	Zoom   Prop[float64]
	// BBox is `west,south,east,north` in degrees, which fits the view.
	BBox Prop[string]
	// Geom is GeoJSON text, or the path of an asset that holds it.
	Geom Prop[string]
	// DisablePan and DisableZoom take the matching pointer gesture away.
	DisablePan  Prop[bool]
	DisableZoom Prop[bool]

	lat, lng, zoom float64
	loaded         bool
	w, h           float64
	// appliedCenter and appliedZoom are the property values the internal camera
	// already follows, so a frame that changed neither skips parsing them.
	appliedCenter string
	appliedZoom   float64
	// fitBBox remembers the box and the size the view was fitted to, so a
	// repaint does not undo a pan.
	fitBBox    string
	fitW, fitH float64

	tiles map[MapTile]*img.Data

	// geomText is the text of a `geom` that names an asset; the ui layer reads
	// it and keeps the property itself untouched, so an expression that returns
	// the path does not fight the resolved text.
	geomSrc  string
	geomText string
	geom     *MapGeometry
	geomErr  error
}

// newMap seeds the map's cells. The defaults match componentdef, so the
// validator and the builder agree on what an unwritten property means.
func newMap() *Map {
	return &Map{
		Component:   newComponent(),
		Tiles:       newProp(nil, "tiles", DirtyPaint, "https://tile.openstreetmap.org/{z}/{x}/{y}.png"),
		Attribution: newProp(nil, "attribution", DirtyPaint, "© OpenStreetMap contributors"),
		Center:      newProp(nil, "center", DirtyPaint, ""),
		Zoom:        newProp(nil, "zoom", DirtyPaint, 2.0),
		BBox:        newProp(nil, "bbox", DirtyPaint, ""),
		Geom:        newProp(nil, "geom", DirtyPaint, ""),
		DisablePan:  newProp(nil, "disablePan", DirtyPaint, false),
		DisableZoom: newProp(nil, "disableZoom", DirtyPaint, false),
	}
}

// Kind implements Node.
func (*Map) Kind() string { return "Map" }

func (m *Map) namedProps() []NamedProp {
	return append(m.propsExcept("Map",
		"tiles", "attribution", "center", "zoom", "bbox", "geom", "disablePan", "disableZoom"),
		StringProp(&m.NodeBase, "tiles", &m.Tiles),
		StringProp(&m.NodeBase, "attribution", &m.Attribution),
		// A view change moves where things are drawn, not how big the map is.
		StringProp(&m.NodeBase, "center", &m.Center),
		NumberProp(&m.NodeBase, "zoom", DirtyPaint, &m.Zoom),
		StringProp(&m.NodeBase, "bbox", &m.BBox),
		StringProp(&m.NodeBase, "geom", &m.Geom),
		BoolProp(&m.NodeBase, "disablePan", DirtyPaint, &m.DisablePan),
		BoolProp(&m.NodeBase, "disableZoom", DirtyPaint, &m.DisableZoom),
	)
}

// TileSize is the side of one tile at the current zoom, in logical pixels. It
// is 256 while the zoom is a whole number.
func (m *Map) TileSize() float64 { return mapTileSize * math.Pow(2, m.zoom-float64(m.TileZoom())) }

// TileZoom is the integer level the tiles are fetched at, the current zoom
// rounded so the bitmap is close to its drawn size.
func (m *Map) TileZoom() int {
	z := int(math.Round(m.zoom))
	if z < 0 {
		return 0
	}
	if z > int(mapMaxZoom) {
		return int(mapMaxZoom)
	}
	return z
}

// View reports the camera the map is showing.
func (m *Map) View() (lat, lng, zoom float64) { return m.lat, m.lng, m.zoom }

// SetView moves the camera. It writes back to `center` and `zoom`, so the two
// properties always describe the frame on screen.
func (m *Map) SetView(lat, lng, zoom float64) { m.setView(lat, lng, zoom) }

// PanBy moves the camera by a pointer delta, in logical pixels. A positive dx
// drags the map right, which moves the view west.
func (m *Map) PanBy(dx, dy float64) {
	if m.w <= 0 || m.h <= 0 {
		return
	}
	scale := m.worldSize()
	x := mercatorX(m.lng) - dx/scale
	y := mercatorY(m.lat) - dy/scale
	m.setView(invMercatorY(y), invMercatorX(x), m.zoom)
}

// ZoomAt changes the zoom while keeping the position under fx,fy (in the map's
// own pixels) where it is.
func (m *Map) ZoomAt(fx, fy, delta float64) {
	if m.w <= 0 || m.h <= 0 || delta == 0 {
		return
	}
	old := m.worldSize()
	ax := mercatorX(m.lng) + (fx-m.w/2)/old
	ay := mercatorY(m.lat) + (fy-m.h/2)/old
	zoom := clampZoom(m.zoom + delta)
	if zoom == m.zoom {
		return
	}
	scale := mapTileSize * math.Pow(2, zoom)
	m.setView(invMercatorY(ay-(fy-m.h/2)/scale), invMercatorX(ax-(fx-m.w/2)/scale), zoom)
}

// Prepare applies the camera properties to a map laid out at w by h and must
// run before ViewTiles. The ui layer calls it once per frame.
func (m *Map) Prepare(w, h float64) {
	if w <= 0 || h <= 0 {
		return
	}
	m.w, m.h = w, h
	if bb := strings.TrimSpace(m.BBox.Get()); bb != "" {
		if bb != m.fitBBox || w != m.fitW || h != m.fitH {
			if p, zoom, ok := fitBounds(bb, w, h); ok {
				m.fitBBox, m.fitW, m.fitH = bb, w, h
				m.setView(p.Lat, p.Lng, zoom)
			}
		}
		return
	}
	m.fitBBox = ""
	center, zoom := m.Center.Get(), clampZoom(m.Zoom.Get())
	if m.loaded && center == m.appliedCenter && zoom == m.appliedZoom {
		return
	}
	m.appliedCenter, m.appliedZoom = center, zoom
	p, has := parseLatLng(center)
	if has {
		m.lat, m.lng = p.Lat, p.Lng
	}
	m.zoom = zoom
	m.loaded = true
}

// ViewTiles lists the tiles the current camera covers, with their rectangles
// relative to the map's top-left corner. Longitudes past the antimeridian wrap
// to the other side of the world.
func (m *Map) ViewTiles() []MapTileView {
	if m.w <= 0 || m.h <= 0 {
		return nil
	}
	z := m.TileZoom()
	n := 1 << z
	size := m.TileSize()
	left := mercatorX(m.lng)*float64(n) - m.w/2/size
	top := mercatorY(m.lat)*float64(n) - m.h/2/size
	x0 := int(math.Floor(left))
	x1 := int(math.Floor(left + m.w/size))
	y0 := max(int(math.Floor(top)), 0)
	y1 := min(int(math.Floor(top+m.h/size)), n-1)
	out := make([]MapTileView, 0, (x1-x0+1)*(y1-y0+1))
	for ty := y0; ty <= y1; ty++ {
		for tx := x0; tx <= x1; tx++ {
			out = append(out, MapTileView{
				MapTile: MapTile{Z: z, X: ((tx % n) + n) % n, Y: ty},
				Left:    (float64(tx) - left) * size,
				Top:     (float64(ty) - top) * size,
				W:       size,
				H:       size,
			})
		}
	}
	return out
}

// Project turns a position into a point relative to the map's top-left corner.
// A position outside the visible area is still projected; the caller clips it.
func (m *Map) Project(p Point) (x, y float64) {
	scale := m.worldSize()
	return (mercatorX(p.Lng)-mercatorX(m.lng))*scale + m.w/2,
		(mercatorY(p.Lat)-mercatorY(m.lat))*scale + m.h/2
}

// ResetTiles clears the bitmaps of the previous frame. The ui layer fills them
// again with SetTile, so a camera move never draws a tile that is no longer in
// view.
func (m *Map) ResetTiles() {
	if len(m.tiles) > 0 {
		clear(m.tiles)
	}
}

// SetTile gives one visible tile its decoded bitmap.
func (m *Map) SetTile(t MapTile, d *img.Data) {
	if d == nil {
		return
	}
	if m.tiles == nil {
		m.tiles = map[MapTile]*img.Data{}
	}
	m.tiles[t] = d
}

// Tile returns the bitmap of a visible tile, or nil while it is loading.
func (m *Map) Tile(t MapTile) *img.Data {
	if m.tiles == nil {
		return nil
	}
	return m.tiles[t]
}

// SetGeomText hands the map the GeoJSON behind a `geom` that names a file. The
// property keeps the path, so a hot reload or an expression can still change it.
func (m *Map) SetGeomText(text string) {
	if m.geomText == text {
		return
	}
	m.geomText = text
	m.geomSrc = ""
	m.Base().MarkDirty(DirtyPaint)
}

// Geometry returns the parsed `geom`, reparsing it only when the text changes.
// Inline GeoJSON answers directly; a path answers with what SetGeomText was
// given.
func (m *Map) Geometry() *MapGeometry {
	src := m.Geom.Get()
	if !looksLikeJSON(src) && m.geomText != "" {
		src = m.geomText
	}
	if src != m.geomSrc {
		m.geomSrc = src
		m.geom, m.geomErr = nil, nil
		if strings.TrimSpace(src) != "" {
			m.geom, m.geomErr = ParseGeoJSON(src)
		}
	}
	return m.geom
}

// looksLikeJSON distinguishes GeoJSON text from a path to it.
func looksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

// GeomError reports why the last `geom` did not parse, or nil.
func (m *Map) GeomError() error { return m.geomErr }

func (m *Map) setView(lat, lng, zoom float64) {
	m.lat, m.lng = clampLat(lat), wrapLng(lng)
	m.zoom = clampZoom(zoom)
	m.loaded = true
	m.appliedCenter = formatLatLng(m.lat, m.lng)
	m.appliedZoom = m.zoom
	m.Center.Set(m.appliedCenter)
	m.Zoom.Set(m.zoom)
	m.Base().MarkDirty(DirtyPaint)
}

func (m *Map) worldSize() float64 { return mapTileSize * math.Pow(2, m.zoom) }

// The map's tile grid. 256 is the size every raster tile service uses.
const (
	mapTileSize = 256.0
	mapMaxZoom  = 19.0
)

// mercatorX is the Web Mercator world x of a longitude, in 0..1.
func mercatorX(lng float64) float64 { return (lng + 180) / 360 }

// mercatorY is the Web Mercator world y of a latitude, in 0..1 from the north.
// The poles cannot be projected, so the usual 85.0511° cut is the limit.
func mercatorY(lat float64) float64 {
	lat = clampLat(lat)
	phi := lat * math.Pi / 180
	return (1 - math.Log(math.Tan(phi)+1/math.Cos(phi))/math.Pi) / 2
}

func invMercatorX(x float64) float64 { return x*360 - 180 }

func invMercatorY(y float64) float64 {
	return math.Atan(math.Sinh(math.Pi*(1-2*y))) * 180 / math.Pi
}

func clampLat(lat float64) float64 {
	return math.Max(-85.05112878, math.Min(85.05112878, lat))
}

// wrapLng brings a longitude back into -180..180, so dragging past the
// antimeridian keeps going instead of running off the world.
func wrapLng(lng float64) float64 {
	for lng > 180 {
		lng -= 360
	}
	for lng < -180 {
		lng += 360
	}
	return lng
}

func clampZoom(z float64) float64 {
	if math.IsNaN(z) {
		return 0
	}
	return math.Max(0, math.Min(mapMaxZoom, z))
}

func formatLatLng(lat, lng float64) string {
	return strconv.FormatFloat(lat, 'f', 6, 64) + "," + strconv.FormatFloat(lng, 'f', 6, 64)
}

// parseLatLng reads the `lat,lon` form people copy from a map.
func parseLatLng(s string) (Point, bool) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return Point{}, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lng, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || math.Abs(lat) > 90 || math.Abs(lng) > 360 {
		return Point{}, false
	}
	return Point{Lat: lat, Lng: wrapLng(lng)}, true
}

// fitBounds turns `west,south,east,north` into the centre and zoom that show
// the whole box, with a little room so the edges are not under the border.
func fitBounds(s string, w, h float64) (Point, float64, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return Point{}, 0, false
	}
	var n [4]float64
	for i := range n {
		v, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		if err != nil {
			return Point{}, 0, false
		}
		n[i] = v
	}
	west, south, east, north := n[0], n[1], n[2], n[3]
	if east < west {
		east += 360 // a box across the antimeridian
	}
	x0, x1 := mercatorX(west), mercatorX(east)
	y0, y1 := mercatorY(north), mercatorY(south)
	if x1 <= x0 || y1 <= y0 {
		return Point{}, 0, false
	}
	zoom := math.Log2(math.Min(w/((x1-x0)*mapTileSize), h/((y1-y0)*mapTileSize)))
	return Point{Lat: invMercatorY((y0 + y1) / 2), Lng: wrapLng(invMercatorX((x0 + x1) / 2))},
		clampZoom(zoom), true
}
