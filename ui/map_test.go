package ui

import (
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestMapTilesLoad covers the tile path end to end: a design points the map at
// an http server, the frames fill in the visible tiles, and the bitmap lands
// without a reload.
func TestMapTilesLoad(t *testing.T) {
	png := testPNG(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	}))
	defer srv.Close()

	a, win, _ := inputFixture(t, `Window {
		Map {
			id: map
			width: 480 height: 320
			tiles: "`+srv.URL+`/{z}/{x}/{y}.png"
			center: "41,29"
			zoom: 4
		}
	}`, 480, 320)
	m := MustByID[*Map](win, "map")
	a.loadImages(win.tree)
	if !a.hasMaps {
		t.Fatal("the tree has a Map but hasMaps is false")
	}

	deadline := time.Now().Add(5 * time.Second)
	loaded := false
	for time.Now().Before(deadline) {
		a.syncMaps()
		a.drainPosted()
		for _, v := range m.ViewTiles() {
			if m.Tile(v.MapTile) != nil {
				loaded = true
				break
			}
		}
		if loaded {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !loaded {
		t.Fatal("no tile landed")
	}
	if hits.Load() == 0 {
		t.Fatal("the tile server was never asked")
	}
	if len(a.mapKeys) > mapTileCacheMax {
		t.Fatalf("cache holds %d tiles, over the %d cap", len(a.mapKeys), mapTileCacheMax)
	}
}

// TestMapGesturesPanAndZoom checks the pointer: a drag moves the camera and a
// wheel zooms it, and each switch takes its own gesture away.
func TestMapGesturesPanAndZoom(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Map { id: map width: 400 height: 300 center: "41,29" zoom: 4 }
	}`, 400, 300)
	m := MustByID[*Map](win, "map")
	// The frame loop prepares a map after layout; the test does the same.
	m.Prepare(m.Bounds().W, m.Bounds().H)
	r := absoluteRect(m)
	cx, cy := r.X+r.W/2, r.Y+r.H/2

	_, lng0, _ := m.View()
	in.MouseDown(cx, cy, 0, false)
	in.MouseMove(cx+40, cy)
	in.MouseUp(cx+40, cy, 0)
	_, lng1, _ := m.View()
	if lng1 >= lng0 {
		t.Fatalf("dragging right moved the view east: %v -> %v", lng0, lng1)
	}

	in.MouseMove(cx, cy)
	_, _, z0 := m.View()
	// The map's wheel is reversed from a page scroll: a notch away from the
	// user zooms in.
	in.Scroll(0, 1, false)
	_, _, z1 := m.View()
	if z1 <= z0 {
		t.Fatalf("wheel did not zoom in: %v -> %v", z0, z1)
	}

	m.DisableZoom.Set(true)
	in.Scroll(0, 1, false)
	if _, _, z2 := m.View(); z2 != z1 {
		t.Fatalf("a disabled zoom still zoomed: %v -> %v", z1, z2)
	}

	m.DisablePan.Set(true)
	_, lng2, _ := m.View()
	in.MouseDown(cx, cy, 0, false)
	in.MouseMove(cx+40, cy)
	in.MouseUp(cx+40, cy, 0)
	if _, lng3, _ := m.View(); lng3 != lng2 {
		t.Fatalf("a disabled pan still moved: %v -> %v", lng2, lng3)
	}
}

// TestMapDoubleClickAndPinch covers the two gestures that are not a drag: a
// second press on the same spot zooms in around it, and a pinch applies its
// relative scale to the map under the pointer.
func TestMapDoubleClickAndPinch(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Map { id: map width: 400 height: 300 center: "41,29" zoom: 4 }
	}`, 400, 300)
	m := MustByID[*Map](win, "map")
	m.Prepare(m.Bounds().W, m.Bounds().H)
	r := absoluteRect(m)
	cx, cy := r.X+r.W/2, r.Y+r.H/2

	_, _, z0 := m.View()
	in.MouseDown(cx, cy, 0, false)
	in.MouseUp(cx, cy, 0)
	in.MouseDown(cx, cy, 0, false)
	if _, _, z1 := m.View(); z1 != z0+1 {
		t.Fatalf("double click zoomed %v -> %v, want one level in", z0, z1)
	}
	in.MouseUp(cx, cy, 0)

	in.MouseMove(cx, cy)
	_, _, z2 := m.View()
	in.Pinch(2)
	_, _, z3 := m.View()
	if math.Abs(z3-(z2+1)) > 1e-9 {
		t.Fatalf("pinch 2x zoomed %v -> %v, want one level in", z2, z3)
	}
	m.DisableZoom.Set(true)
	in.Pinch(2)
	if _, _, z4 := m.View(); z4 != z3 {
		t.Fatalf("a disabled pinch zoomed: %v -> %v", z3, z4)
	}
}

// TestMapPansOverOverlays is the Geom regression: a Geom covers the whole map,
// so the pointer press hits it rather than the Map; the drag still has to move
// the map.
func TestMapPansOverOverlays(t *testing.T) {
	_, win, in := inputFixture(t, `Window {
		Map {
			id: map width: 400 height: 300 center: "41,29" zoom: 5
			Geom { data: [[28, 40], [30, 40], [30, 42], [28, 42]] }
		}
	}`, 400, 300)
	m := MustByID[*Map](win, "map")
	m.Prepare(m.Bounds().W, m.Bounds().H)
	r := absoluteRect(m)
	cx, cy := r.X+r.W/2, r.Y+r.H/2

	_, lng0, _ := m.View()
	in.MouseDown(cx, cy, 0, false)
	in.MouseMove(cx+40, cy)
	in.MouseUp(cx+40, cy, 0)
	if _, lng1, _ := m.View(); lng1 >= lng0 {
		t.Fatalf("dragging over a Geom did not pan: %v -> %v", lng0, lng1)
	}
}
