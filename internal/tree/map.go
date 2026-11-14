package tree

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/schema"
)

// LatLng is one WGS84 position, in degrees.
type LatLng struct{ Lat, Lng float64 }

// MapTile is one tile in the slippy-map scheme: a zoom level and an x/y index
// inside it.
type MapTile struct{ Z, X, Y int }

// MapTileView is a tile together with the rectangle it covers inside the map,
// in the map's own pixels.
type MapTileView struct {
	MapTile
	Left, Top, W, H float64
}

// Map draws a slippy map from tiles, with Geom, Path and Point children drawn
// over it. By default the tiles come from OpenStreetMap; `tiles` takes any
// `{z}/{x}/{y}` template.
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
	// BBox is [west, south, east, north] in degrees. While it has four items it
	// frames the view and wins over center and zoom.
	BBox ListProp[float64]
	// ReverseTileColors inverts the tile RGB after decoding, which is what a
	// dark theme over a light raster service needs.
	ReverseTileColors Prop[bool]
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
	fitBBox    []float64
	fitW, fitH float64

	tiles map[MapTile]*img.Data
}

// newMap seeds the map's cells. The defaults match componentdef, so the
// validator and the builder agree on what an unwritten property means.
func newMap() *Map {
	return &Map{
		Component:         newComponent(),
		Tiles:             newProp(nil, "tiles", DirtyPaint, "https://tile.openstreetmap.org/{z}/{x}/{y}.png"),
		Attribution:       newProp(nil, "attribution", DirtyPaint, "© OpenStreetMap contributors"),
		Center:            newProp(nil, "center", DirtyPaint, ""),
		Zoom:              newProp(nil, "zoom", DirtyPaint, 2.0),
		BBox:              ListProp[float64]{parse: listParse[float64](schema.TypeNumber)},
		ReverseTileColors: newProp(nil, "reverseTileColors", DirtyPaint, false),
		DisablePan:        newProp(nil, "disablePan", DirtyPaint, false),
		DisableZoom:       newProp(nil, "disableZoom", DirtyPaint, false),
	}
}

// Kind implements Node.
func (*Map) Kind() string { return "Map" }

func (m *Map) namedProps() []NamedProp {
	return append(m.propsExcept("Map",
		"tiles", "attribution", "center", "zoom", "bbox", "disablePan", "disableZoom"),
		StringProp(&m.NodeBase, "tiles", &m.Tiles),
		StringProp(&m.NodeBase, "attribution", &m.Attribution),
		// A view change moves where things are drawn, not how big the map is.
		StringProp(&m.NodeBase, "center", &m.Center),
		NumberProp(&m.NodeBase, "zoom", DirtyPaint, &m.Zoom),
		ListPropOf(&m.NodeBase, "bbox", DirtyPaint, &m.BBox),
		BoolProp(&m.NodeBase, "reverseTileColors", DirtyPaint, &m.ReverseTileColors),
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
	if bb := m.BBox.Get(); len(bb) == 4 {
		if !slices.Equal(bb, m.fitBBox) || w != m.fitW || h != m.fitH {
			if p, zoom, ok := fitBounds(bb, w, h); ok {
				m.fitBBox, m.fitW, m.fitH = slices.Clone(bb), w, h
				m.setView(p.Lat, p.Lng, zoom)
			}
		}
		return
	}
	m.fitBBox = nil
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
func (m *Map) Project(lat, lng float64) (x, y float64) {
	return m.ProjectWorld(mercatorX(lng), mercatorY(lat))
}

// ProjectWorld is Project for a normalized Mercator point, which is what a
// rasterized fill is laid out in.
func (m *Map) ProjectWorld(x, y float64) (sx, sy float64) {
	scale := m.worldSize()
	return (x-mercatorX(m.lng))*scale + m.w/2,
		(y-mercatorY(m.lat))*scale + m.h/2
}

// PointAnchor is where a Point child of this map lands, relative to the map's
// top-left corner. ok is false until the coordinate has two numbers, which is
// how a marker with no selection stays off the map.
func (m *Map) PointAnchor(p *Point) (x, y float64, ok bool) {
	c := p.Coordinate.Get()
	if len(c) < 2 {
		return 0, 0, false
	}
	x, y = m.Project(c[1], c[0])
	return x, y, true
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
	mapTileSize  = 256.0
	mapMaxZoom   = 19.0
	mapFitMargin = 0.9
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
func parseLatLng(s string) (LatLng, bool) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return LatLng{}, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lng, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || math.Abs(lat) > 90 || math.Abs(lng) > 360 {
		return LatLng{}, false
	}
	return LatLng{Lat: lat, Lng: wrapLng(lng)}, true
}

// fitBounds turns `west,south,east,north` into the centre and zoom that show
// the whole box, with a little room so the edges are not under the border.
func fitBounds(n []float64, w, h float64) (LatLng, float64, bool) {
	if len(n) != 4 {
		return LatLng{}, 0, false
	}
	west, south, east, north := n[0], n[1], n[2], n[3]
	if east < west {
		east += 360 // a box across the antimeridian
	}
	x0, x1 := mercatorX(west), mercatorX(east)
	y0, y1 := mercatorY(north), mercatorY(south)
	if x1 <= x0 || y1 <= y0 {
		return LatLng{}, 0, false
	}
	// A tenth of the box stays as a margin, so the boundary a bbox frames is
	// not drawn on the map's own edge.
	zoom := math.Log2(mapFitMargin * math.Min(w/((x1-x0)*mapTileSize), h/((y1-y0)*mapTileSize)))
	return LatLng{Lat: invMercatorY((y0 + y1) / 2), Lng: wrapLng(invMercatorX((x0 + x1) / 2))},
		clampZoom(zoom), true
}
