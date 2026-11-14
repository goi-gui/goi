package paint

import (
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// mapList builds a map from a design snippet and prepares its camera, which the
// ui layer normally does after layout.
func mapList(t *testing.T, src string, w, h float64) (*tree.Tree, *tree.Map, *List) {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	var m *tree.Map
	for _, n := range tr.All() {
		if mm, ok := n.(*tree.Map); ok {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("no map in the design")
	}
	m.Prepare(w, h)
	layout.New(nil).Layout(tr.Root, w, h)
	l := &List{}
	l.Build(tr, w, h)
	return tr, m, l
}

// TestMapPaintsTilesAndAttribution checks the tile layer: a visible tile with a
// bitmap becomes a bitmap op, and the credit line is drawn over the corner.
func TestMapPaintsTilesAndAttribution(t *testing.T) {
	tr, m, l := mapList(t, `Window {
		Map {
			width: 300 height: 200
			center: "41,29"
			zoom: 4
			attribution: "© OpenStreetMap"
		}
	}`, 300, 200)
	tiles := m.ViewTiles()
	if len(tiles) == 0 {
		t.Fatal("no visible tiles")
	}
	d := &img.Data{W: 256, H: 256, Pix: make([]byte, 256*256*4)}
	m.SetTile(tiles[0].MapTile, d)
	l.Build(tr, 300, 200)

	bitmaps, texts := 0, 0
	for _, op := range l.Ops {
		switch op.Kind {
		case Bitmap:
			if op.Src == d {
				bitmaps++
			}
		case Text:
			if op.Text == "© OpenStreetMap" {
				texts++
			}
		}
	}
	if bitmaps != 1 {
		t.Fatalf("bitmap ops = %d, want the one tile", bitmaps)
	}
	if texts != 1 {
		t.Fatalf("attribution ops = %d", texts)
	}
}

// TestMapPaintsGeom checks that a Geom child reaches the display list as one
// stroke per segment, in the polygon's own order.
func TestMapPaintsGeom(t *testing.T) {
	src := `Window {
		Map {
			width: 300 height: 200
			center: "41,29" zoom: 5
			Geom { data: [28, 40, 30, 40, 30, 42, 28, 42] }
		}
	}`
	_, _, l := mapList(t, src, 300, 200)
	strokes := 0
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Rect[3] == 2 {
			strokes++
		}
	}
	if strokes != 4 {
		t.Fatalf("strokes = %d, want four sides:\n%s", strokes, l.Dump())
	}
}

// TestMapColorsAndFill checks the Geom colours: the ring takes lineColor and a
// fillColor produces one rasterized fill under the strokes.
func TestMapColorsAndFill(t *testing.T) {
	src := `Window {
		Map {
			width: 300 height: 200
			center: "41,29" zoom: 5
			Geom {
				data:      [28, 40, 30, 40, 30, 42, 28, 42]
				lineColor: #ff0000
				fillColor: #00ff0044
			}
		}
	}`
	_, _, l := mapList(t, src, 300, 200)
	var strokes, fills int
	for _, op := range l.Ops {
		switch {
		case op.Kind == Bitmap && op.Src != nil:
			fills++
		case op.Kind == Fill && op.Rect[3] == 2:
			strokes++
			if op.Color != 0xFF0000FF {
				t.Fatalf("line colour = %#x, want #ff0000", op.Color)
			}
		}
	}
	if strokes != 4 || fills != 1 {
		t.Fatalf("strokes = %d fills = %d, want 4 and 1:\n%s", strokes, fills, l.Dump())
	}
}

// TestMapTilesDoNotRoundTheirOwnCorners guards the seam the map's radius used
// to leave: four adjacent tiles rounded by 16dp cut a star-shaped hole where
// their corners meet. Only the shared clip may be rounded.
func TestMapTilesDoNotRoundTheirOwnCorners(t *testing.T) {
	tr, m, l := mapList(t, `Window {
		Map {
			width: 300 height: 200 radius: 16
			center: "41,29" zoom: 4
		}
	}`, 300, 200)
	tiles := m.ViewTiles()
	d := &img.Data{W: 256, H: 256, Pix: make([]byte, 256*256*4)}
	m.SetTile(tiles[0].MapTile, d)
	l.Build(tr, 300, 200)

	found := false
	for _, op := range l.Ops {
		if op.Kind != Bitmap || op.Src != d {
			continue
		}
		found = true
		if op.Radius != [4]float32{} {
			t.Fatalf("tile rounds its own corners: %v", op.Radius)
		}
		if op.ClipRadius != 16 || op.ClipBox == [4]float32{} {
			t.Fatalf("tile clip = %v box %v, want the map's 16dp round", op.ClipRadius, op.ClipBox)
		}
	}
	if !found {
		t.Fatal("no tile painted")
	}
}

// TestMapReverseTileColors checks that the invert switch reaches the tile op.
func TestMapReverseTileColors(t *testing.T) {
	tr, m, l := mapList(t, `Window {
		Map {
			width: 300 height: 200 center: "41,29" zoom: 3
			reverseTileColors: true
		}
	}`, 300, 200)
	tiles := m.ViewTiles()
	d := &img.Data{W: 256, H: 256, Pix: make([]byte, 256*256*4)}
	m.SetTile(tiles[0].MapTile, d)
	l.Build(tr, 300, 200)
	found := false
	for _, op := range l.Ops {
		if op.Kind == Bitmap && op.Src == d {
			found = true
			if !op.Invert {
				t.Fatal("tile was not inverted")
			}
		}
	}
	if !found {
		t.Fatal("no tile painted")
	}
}

// TestMapPointIsAnchored checks the marker overlay: a Point's child is drawn
// centred on its coordinate, not at the map's origin.
func TestMapPointIsAnchored(t *testing.T) {
	src := `Window {
		Map {
			width: 300 height: 200 center: "41,29" zoom: 5
			Point {
				coordinate: [28, 40]
				Rectangle { width: 10 height: 10 background: #ff0000 }
			}
		}
	}`
	_, m, l := mapList(t, src, 300, 200)
	wantX, wantY := m.Project(40, 28)
	found := false
	for _, op := range l.Ops {
		if op.Kind != Fill || op.Color != 0xFF0000FF {
			continue
		}
		found = true
		cx := float64(op.Rect[0]) + float64(op.Rect[2])/2
		cy := float64(op.Rect[1]) + float64(op.Rect[3])/2
		if math.Abs(cx-wantX) > 0.01 || math.Abs(cy-wantY) > 0.01 {
			t.Fatalf("marker at %v,%v, want the projected %v,%v", cx, cy, wantX, wantY)
		}
	}
	if !found {
		t.Fatal("the marker was not painted")
	}
}

// TestMapPathStrokes checks that a Path becomes one stroke per pair of
// coordinates.
func TestMapPathStrokes(t *testing.T) {
	src := `Window {
		Map {
			id:     map
			width:  300 height: 200
			center: "41,29" zoom: 5
			Path { id: route }
		}
	}`
	tr, _, l := mapList(t, src, 300, 200)
	n, ok := tree.Find(tr.Root, "route")
	if !ok {
		t.Fatal("the route path was not found")
	}
	path := n.(*tree.Path)
	path.Coordinates.Set([]tree.Coord{{28, 40}, {29, 40}, {30, 41}})
	l.Build(tr, 300, 200)
	strokes := 0
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Rect[3] == 2 {
			strokes++
		}
	}
	if strokes != 2 {
		t.Fatalf("path strokes = %d, want two segments:\n%s", strokes, l.Dump())
	}
}
