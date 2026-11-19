package paint

import (
	"math"

	"github.com/goi-gui/goi/internal/tree"
)

// paintMap draws the tile layer, the credit line and the map's own border. Its
// Geom, Path and Point children paint themselves on top: they are overlays, not
// content, so they receive the whole map box from layout.
func (l *List) paintMap(m *tree.Map, r tree.Rect, clip tree.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	radius := float32(m.Base().Radius())
	inner := intersect(clip, r)
	box := rectArr(r)
	// The node's own box shows through the gaps while the first tiles load, so
	// an unset background falls back to the theme's surface. paintComponentBox
	// swaps it for the design's when one is given.
	l.fillRect(r, l.Theme.Colors.SurfaceContainerHighest, m.Base().Radius(), 0, 0, inner)
	for _, t := range m.ViewTiles() {
		d := m.Tile(t.MapTile)
		if d == nil || d.W == 0 || d.H == 0 {
			continue
		}
		// Half a pixel of overlap hides the seam a bilinear sampler leaves
		// between two tiles at a fractional zoom.
		l.Ops = append(l.Ops, Op{
			Kind: Bitmap, Src: d, UV: [4]float32{0, 0, 1, 1}, Color: 0xFFFFFFFF,
			Rect:       rectArr(tree.Rect{X: r.X + t.Left, Y: r.Y + t.Top, W: t.W + 0.5, H: t.H + 0.5}),
			Clip:       clipArr(inner),
			ClipBox:    box,
			ClipRadius: radius,
			Invert:     m.ReverseTileColors.Get(),
		})
	}
	l.paintMapAttribution(m, r, inner)
}

// paintGeom draws one polygon: its rasterized fill first, then the ring as one
// thin rounded quad per segment. The map supplies the camera; the Geom's own
// bounds are the whole map box, so r is also the map's box.
func (l *List) paintGeom(g *tree.Geom, r tree.Rect, clip tree.Rect) {
	m := mapParent(g.Base())
	if m == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	radius := float32(m.Base().Radius())
	box := rectArr(r)
	l.paintGeomFill(g, m, r, clip, radius, box)
	color := l.Theme.Colors.Primary
	if c := g.LineColor.Get(); c != 0 {
		color = c
	}
	ring := g.Ring()
	for i := 1; i < len(ring); i++ {
		l.mapSegment(m, r, clip, radius, box, ring[i-1], ring[i], color)
	}
}

// paintGeomFill draws the polygon's mask. It is baked in Mercator world
// coordinates, so panning only moves its quad; the part outside the map is
// cropped with the op's UV, which keeps a zoomed-in polygon from packing a
// whole-country bitmap at screen size.
func (l *List) paintGeomFill(g *tree.Geom, m *tree.Map, r tree.Rect, clip tree.Rect, radius float32, box [4]float32) {
	d, wb := g.Fill()
	if d == nil {
		return
	}
	x0, y0 := m.ProjectWorld(wb[0], wb[1])
	x1, y1 := m.ProjectWorld(wb[2], wb[3])
	quad := tree.Rect{X: r.X + x0, Y: r.Y + y0, W: x1 - x0, H: y1 - y0}
	vis := intersect(clip, quad)
	if quad.W <= 0 || quad.H <= 0 || vis.W <= 0 || vis.H <= 0 {
		return
	}
	u0 := float32((vis.X - quad.X) / quad.W)
	v0 := float32((vis.Y - quad.Y) / quad.H)
	u1 := float32((vis.Right() - quad.X) / quad.W)
	v1 := float32((vis.Bottom() - quad.Y) / quad.H)
	l.Ops = append(l.Ops, Op{
		Kind: Bitmap, Src: d, UV: [4]float32{u0, v0, u1, v1}, Color: 0xFFFFFFFF,
		Rect:       rectArr(vis),
		Clip:       clipArr(clip),
		ClipBox:    box,
		ClipRadius: radius,
	})
}

// paintPath strokes an open polyline between its coordinates.
func (l *List) paintPath(p *tree.Path, r tree.Rect, clip tree.Rect) {
	m := mapParent(p.Base())
	if m == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	radius := float32(m.Base().Radius())
	box := rectArr(r)
	color := l.Theme.Colors.Primary
	if c := p.LineColor.Get(); c != 0 {
		color = c
	}
	pts := p.Points()
	for i := 1; i < len(pts); i++ {
		l.mapSegment(m, r, clip, radius, box, pts[i-1], pts[i], color)
	}
}

// mapSegment appends one line segment as a thin, turned rounded rectangle. The
// renderer already rotates a quad, so a boundary needs no path primitive.
func (l *List) mapSegment(m *tree.Map, r tree.Rect, clip tree.Rect, radius float32, box [4]float32, a, b tree.LatLng, color uint32) {
	const width = 2.0
	x1, y1 := m.Project(a.Lat, a.Lng)
	x2, y2 := m.Project(b.Lat, b.Lng)
	x1, y1 = r.X+x1, r.Y+y1
	x2, y2 = r.X+x2, r.Y+y2
	dx, dy := x2-x1, y2-y1
	length := math.Hypot(dx, dy)
	if length < 0.5 || !segmentVisible(x1, y1, x2, y2, r, width) {
		return
	}
	cx, cy := (x1+x2)/2, (y1+y2)/2
	l.Ops = append(l.Ops, Op{
		Kind: Fill,
		Rect: [4]float32{
			float32(cx - length/2), float32(cy - width/2),
			float32(length), float32(width),
		},
		Radius:     [4]float32{width / 2, width / 2, width / 2, width / 2},
		Color:      color,
		Clip:       clipArr(clip),
		ClipRadius: radius,
		ClipBox:    box,
		Rotate:     float32(math.Atan2(dy, dx)),
		PivotX:     float32(cx),
		PivotY:     float32(cy),
	})
}

// mapParent is the Map a Geom or Path belongs to.
func mapParent(b *tree.NodeBase) *tree.Map {
	for p := b.Parent(); p != nil; p = p.Base().Parent() {
		if m, ok := p.(*tree.Map); ok {
			return m
		}
	}
	return nil
}

// segmentVisible reports whether a segment can touch the map, with its stroke
// width taken into account.
func segmentVisible(x1, y1, x2, y2 float64, r tree.Rect, width float64) bool {
	if maxf(x1, x2) < r.X-width || minf(x1, x2) > r.Right()+width ||
		maxf(y1, y2) < r.Y-width || minf(y1, y2) > r.Bottom()+width {
		return false
	}
	return true
}

// paintMapAttribution draws the credit line. OSM's tile policy requires it, so
// it is on unless the design clears `attribution`; it stays readable over both
// light and dark tiles by sitting on its own dark pill.
func (l *List) paintMapAttribution(m *tree.Map, r tree.Rect, clip tree.Rect) {
	s := m.Attribution.Get()
	if s == "" {
		return
	}
	st := l.style("labelSmall")
	w := textWidth(l, s, st) + 10
	h := st.Size + 6
	box := tree.Rect{X: r.Right() - w - 4, Y: r.Bottom() - h - 4, W: w, H: h}
	if box.W > r.W {
		return
	}
	l.fillRect(box, 0x00000099, 4, 0, 0, intersect(clip, r))
	l.textIn(s, st, 0xFFFFFFFF, box, AlignCenter, intersect(clip, r))
}
