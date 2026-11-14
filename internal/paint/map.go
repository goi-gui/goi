package paint

import (
	"math"

	"github.com/goi-gui/goi/internal/tree"
)

// paintMap draws the tile layer, the boundary overlay and the credit line. The
// tiles and the geometry share one clip, rounded to the map's own corners, so a
// `radius` on the map cuts the tiles as well as its background.
func (l *List) paintMap(m *tree.Map, r tree.Rect, clip tree.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	radius := float32(m.Base().Radius())
	inner := intersect(clip, r)
	box := rectArr(r)
	// The node's own box shows through the gaps while the first tiles load, so
	// an unset background falls back to the theme's surface.
	if m.Base().Background() == 0 && m.Base().Gradient("background") == nil {
		l.fillRect(r, l.Theme.Colors.SurfaceContainerHighest, m.Base().Radius(), 0, 0, inner)
	} else {
		l.paintBox(&m.NodeBase, r, clip)
	}
	for _, t := range m.ViewTiles() {
		d := m.Tile(t.MapTile)
		if d == nil || d.W == 0 || d.H == 0 {
			continue
		}
		// Half a pixel of overlap hides the seam a bilinear sampler leaves
		// between two tiles at a fractional zoom.
		l.Ops = append(l.Ops, Op{
			Kind: Bitmap, Src: d, UV: [4]float32{0, 0, 1, 1},
			Rect:       rectArr(tree.Rect{X: r.X + t.Left, Y: r.Y + t.Top, W: t.W + 0.5, H: t.H + 0.5}),
			Clip:       clipArr(inner),
			ClipBox:    box,
			ClipRadius: radius,
			Invert:     m.ReverseTileColors.Get(),
		})
	}
	l.paintMapFill(m, r, inner, radius, box)
	l.paintMapGeom(m, r, inner, radius, box)
	l.paintMapAttribution(m, r, inner)
	// The border goes last: the tiles are full bleed, so one drawn with the
	// background would never be seen.
	if m.Base().UniformBorder() {
		cols := m.Base().BorderColors()
		if cols[0] != 0 && m.Base().BorderEdges().Top > 0 {
			l.fillShape(r, 0, nil, m.Base().Radii(), cols[0], m.Base().BorderEdges().Top, clip)
		}
	} else {
		l.paintSideBorders(&m.NodeBase, r, m.Base().Radii(), m.Base().BorderEdges(), m.Base().BorderColors(), clip)
	}
}

// paintMapFill draws the rasterized polygon fill under the boundary. The mask
// is baked in Mercator world coordinates, so panning only moves its quad; the
// part outside the map is cropped with the op's UV, which keeps a zoomed-in
// polygon from packing a whole-country bitmap at screen size.
func (l *List) paintMapFill(m *tree.Map, r tree.Rect, clip tree.Rect, radius float32, box [4]float32) {
	d, wb := m.PolygonFill()
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
		Kind: Bitmap, Src: d, UV: [4]float32{u0, v0, u1, v1},
		Rect:       rectArr(vis),
		Clip:       clipArr(clip),
		ClipBox:    box,
		ClipRadius: radius,
	})
}

// paintMapGeom strokes each ring and line as one thin rounded rectangle per
// segment, turned about its own midpoint. The renderer already rotates a quad,
// so a line needs no path primitive; a dot at every vertex rounds the joins and
// makes a boundary read as one stroke.
func (l *List) paintMapGeom(m *tree.Map, r tree.Rect, clip tree.Rect, radius float32, box [4]float32) {
	g := m.Geometry()
	if g.Empty() {
		return
	}
	const width = 2.0
	color := l.Theme.Colors.Primary
	if c := m.LineColor.Get(); c != 0 {
		color = c
	}
	stroke := func(a, b tree.Point) {
		x1, y1 := m.Project(a)
		x2, y2 := m.Project(b)
		x1, y1 = r.X+x1, r.Y+y1
		x2, y2 = r.X+x2, r.Y+y2
		dx, dy := x2-x1, y2-y1
		length := math.Hypot(dx, dy)
		if length < 0.5 {
			return
		}
		// One bounding-box test for the whole segment; a long boundary is
		// mostly offscreen and this is what keeps that cheap.
		if !segmentVisible(x1, y1, x2, y2, r, width) {
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
	dot := func(p tree.Point) {
		x, y := m.Project(p)
		x, y = r.X+x, r.Y+y
		if x < r.X-width || x > r.Right()+width || y < r.Y-width || y > r.Bottom()+width {
			return
		}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: Circle,
			Rect:       [4]float32{float32(x - width), float32(y - width), float32(width * 2), float32(width * 2)},
			Color:      color,
			Clip:       clipArr(clip),
			ClipRadius: radius,
			ClipBox:    box,
		})
	}
	for _, poly := range g.Polys {
		for _, ring := range poly {
			for i := 1; i < len(ring); i++ {
				stroke(ring[i-1], ring[i])
			}
			if len(ring) > 2 {
				last := ring[len(ring)-1]
				if last != ring[0] {
					stroke(last, ring[0])
				}
			}
			for _, p := range ring {
				dot(p)
			}
		}
	}
	for _, line := range g.Lines {
		for i := 1; i < len(line); i++ {
			stroke(line[i-1], line[i])
		}
	}
	for _, p := range g.Points {
		dot(p)
	}
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
