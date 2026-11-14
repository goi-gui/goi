package tree

import (
	"math"
	"sort"

	"github.com/goi-gui/goi/internal/img"
)

// fillPixels is the longest side of the mask a polygon fill bakes. It is a
// one-time raster per geometry and colour, not per frame: panning and zooming
// only move the quad the mask is drawn into.
const fillPixels = 1024

// PolygonFill returns the rasterized `fillColor` for the polygons in `geom`,
// with the normalized Mercator box it covers: [west, north, east, south]. It
// returns nil when there is no colour or nothing to fill.
//
// The mask is cached on the geometry pointer, so a paint can call this every
// frame and only the first one does any work.
func (m *Map) PolygonFill() (*img.Data, [4]float64) {
	color := m.FillColor.Get()
	g := m.Geometry()
	if color&0xFF == 0 || g.Empty() || len(g.Polys) == 0 {
		m.fill, m.fillSrc, m.fillColor = nil, nil, 0
		return nil, [4]float64{}
	}
	if m.fillSrc == g && m.fillColor == color {
		return m.fill, m.fillBox
	}
	m.fillSrc, m.fillColor = g, color
	m.fill, m.fillBox = fillMask(g, color)
	return m.fill, m.fillBox
}

// fillMask rasterizes polygons with an even-odd scanline fill, which turns a
// ring and its holes into one mask. Longitudes and latitudes become Mercator
// world coordinates first, so the quad the mask is drawn into stays linear
// under the map's own projection.
func fillMask(g *MapGeometry, color uint32) (*img.Data, [4]float64) {
	minX, minY := 1.0, 1.0
	maxX, maxY := 0.0, 0.0
	for _, poly := range g.Polys {
		for _, ring := range poly {
			for _, p := range ring {
				x, y := mercatorX(p.Lng), mercatorY(p.Lat)
				minX, maxX = math.Min(minX, x), math.Max(maxX, x)
				minY, maxY = math.Min(minY, y), math.Max(maxY, y)
			}
		}
	}
	dx, dy := maxX-minX, maxY-minY
	if dx <= 0 || dy <= 0 {
		return nil, [4]float64{}
	}
	pw, ph := fillPixels, fillPixels
	if dx >= dy {
		ph = max(8, int(math.Round(fillPixels*dy/dx)))
	} else {
		pw = max(8, int(math.Round(fillPixels*dx/dy)))
	}
	d := &img.Data{W: pw, H: ph, Pix: make([]byte, pw*ph*4)}
	r, gc, b, a := byte(color>>24), byte(color>>16), byte(color>>8), byte(color)
	xs := make([]float64, 0, 64)
	for row := 0; row < ph; row++ {
		// Sample at the pixel centre, so a shape that fills its own box keeps
		// the whole edge row.
		wy := minY + (float64(row)+0.5)/float64(ph)*dy
		for _, poly := range g.Polys {
			xs = xs[:0]
			for _, ring := range poly {
				n := len(ring)
				if n < 2 {
					continue
				}
				for i := 1; i <= n; i++ {
					a, b := ring[i-1], ring[i%n]
					ya, yb := mercatorY(a.Lat), mercatorY(b.Lat)
					if (ya <= wy && wy < yb) || (yb <= wy && wy < ya) {
						t := (wy - ya) / (yb - ya)
						xs = append(xs, mercatorX(a.Lng)+t*(mercatorX(b.Lng)-mercatorX(a.Lng)))
					}
				}
			}
			if len(xs) < 2 {
				continue
			}
			sort.Float64s(xs)
			for i := 0; i+1 < len(xs); i += 2 {
				x0, x1 := math.Max(xs[i], minX), math.Min(xs[i+1], maxX)
				if x1 <= x0 {
					continue
				}
				px0 := int((x0 - minX) / dx * float64(pw))
				px1 := int(math.Ceil((x1 - minX) / dx * float64(pw)))
				px0, px1 = max(px0, 0), min(px1, pw)
				for x := px0; x < px1; x++ {
					i := (row*pw + x) * 4
					d.Pix[i], d.Pix[i+1], d.Pix[i+2], d.Pix[i+3] = r, gc, b, a
				}
			}
		}
	}
	return d, [4]float64{minX, minY, maxX, maxY}
}
