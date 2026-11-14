package paint

import (
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

// TestMapPaintsGeom checks that a polygon reaches the display list as strokes
// that turn, which is how a line is drawn without a path primitive.
func TestMapPaintsGeom(t *testing.T) {
	src := `Window {
		Map {
			width: 300 height: 200
			center: "41,29" zoom: 5
			geom: "{\"type\":\"Polygon\",\"coordinates\":[[[28,40],[30,40],[30,42],[28,42],[28,40]]]}"
		}
	}`
	_, _, l := mapList(t, src, 300, 200)
	strokes, dots := 0, 0
	for _, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		switch {
		case op.Shape == Circle:
			dots++
		case op.Rect[3] == 2: // the boundary stroke width
			strokes++
		}
	}
	// Four sides; the ring repeats its first vertex, which also takes a dot.
	if strokes != 4 || dots != 5 {
		t.Fatalf("strokes = %d dots = %d, want 4 and 5:\n%s", strokes, dots, l.Dump())
	}
}
