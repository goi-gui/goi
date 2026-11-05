package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/ksckaan1/goi/internal/layout"
	"github.com/ksckaan1/goi/internal/tree"
)

// svgFixture is a 10x20 rectangle that follows currentColor.
const svgFixture = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 20">
	<rect width="10" height="20" fill="currentColor"/>
</svg>`

func svgApp(t *testing.T, src string) (*App, *Window) {
	t.Helper()
	a := newHeadlessApp()
	a.assets = fstest.MapFS{"icon.svg": &fstest.MapFile{Data: []byte(svgFixture)}}
	tr, diag := tree.BuildSource([]byte(src), &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	w := &Window{tree: tr, app: a, title: tr.Root.Title.Get()}
	a.win = w
	a.loadImages(tr)
	return a, w
}

// TestSVGLoadsAndFollowsColor covers the whole simple path: the file is
// rasterized with the design colour, and Color.Set rasters again.
func TestSVGLoadsAndFollowsColor(t *testing.T) {
	a, w := svgApp(t, `Window {
		Column {
			SVG { id: icon src: "icon.svg" color: #ff0000 }
		}
	}`)
	n := MustByID[*SVG](w, "icon")
	if n.LoadErr != nil {
		t.Fatalf("LoadErr = %v", n.LoadErr)
	}
	if n.Data == nil {
		t.Fatal("the SVG was not rasterized")
	}
	if n.IntrinsicW != 10 || n.IntrinsicH != 20 {
		t.Fatalf("intrinsic = %vx%v, want 10x20", n.IntrinsicW, n.IntrinsicH)
	}
	first := n.Data

	n.Color.Set(0x0000FFFF)
	a.syncSVGs()
	if n.Data == first {
		t.Fatal("a new colour reused the old raster")
	}
	if r, g, b, _ := svgPixel(n, n.Data.W/2, n.Data.H/2); r > 15 || g > 15 || b < 240 {
		t.Fatalf("centre = %d,%d,%d, want blue", r, g, b)
	}
}

// TestSVGRemoteLoads mirrors the Image path: the node lays out at once, shows
// the placeholder, and the raster lands without a reload.
func TestSVGRemoteLoads(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(svgFixture))
	}))
	defer srv.Close()

	src := srv.URL + "/icon.svg"
	a, win := svgApp(t, `Window { Column {
		SVG { id: one src: "`+src+`" color: #ff0000 width: 20 height: 40 }
		SVG { id: two src: "`+src+`" color: #ff0000 width: 20 height: 40 }
	} }`)
	one := MustByID[*SVG](win, "one")
	two := MustByID[*SVG](win, "two")

	if one.Data != nil || !one.Loading {
		t.Fatal("a remote src was not marked loading")
	}
	if !a.pulsing {
		t.Fatal("the placeholder was not started")
	}
	pumpPosted(t, a, func() bool { return one.Data != nil && two.Data != nil })

	if hits != 1 {
		t.Fatalf("%d requests for one URL, want 1", hits)
	}
	if one.Loading || one.LoadErr != nil {
		t.Fatalf("Loading = %v, LoadErr = %v", one.Loading, one.LoadErr)
	}
	if one.IntrinsicW != 10 || one.IntrinsicH != 20 {
		t.Fatalf("intrinsic = %vx%v, want 10x20", one.IntrinsicW, one.IntrinsicH)
	}
	if r, g, b, _ := svgPixel(one, one.Data.W/2, one.Data.H/2); r < 240 || g > 15 || b > 15 {
		t.Fatalf("centre = %d,%d,%d, want red", r, g, b)
	}
}

// TestSVGRemoteFailureStopsLoading keeps a bad URL from pulsing forever.
func TestSVGRemoteFailureStopsLoading(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	a, win := svgApp(t, `Window { Column {
		SVG { id: icon src: "`+srv.URL+`/gone.svg" width: 10 height: 10 }
	} }`)
	n := MustByID[*SVG](win, "icon")
	pumpPosted(t, a, func() bool { return !n.Loading })
	if n.LoadErr == nil {
		t.Fatal("the failure was not recorded")
	}
	if n.Data != nil {
		t.Fatal("a 404 produced a raster")
	}
}

// TestSVGColorBeatsRole is the gallery case: the design names a role, Go sets a
// literal, and the frame loop's colour resolver must not undo it.
func TestSVGColorBeatsRole(t *testing.T) {
	a, w := svgApp(t, `Window {
		Column {
			SVG { id: icon src: "icon.svg" color: primary }
		}
	}`)
	n := MustByID[*SVG](w, "icon")
	// The frame resolves the role before it repaints, exactly as the driver does.
	a.win.tree.ResolveColors(themeFor(w).Colors.Role)
	a.syncSVGs()

	n.Color.Set(0x0000FFFF)
	a.win.tree.ResolveColors(themeFor(w).Colors.Role)
	a.syncSVGs()
	if n.Color.Get() != 0x0000FFFF {
		t.Fatalf("colour = %08x, the role put its own back", n.Color.Get())
	}
	if r, g, b, _ := svgPixel(n, n.Data.W/2, n.Data.H/2); r > 15 || g > 15 || b < 240 {
		t.Fatalf("centre = %d,%d,%d, want blue", r, g, b)
	}
}

// TestSVGUsesViewBoxAsIntrinsicSize keeps width and height optional.
func TestSVGUsesViewBoxAsIntrinsicSize(t *testing.T) {
	_, w := svgApp(t, `Window {
		Column {
			SVG { id: icon src: "icon.svg" }
		}
	}`)
	n := MustByID[*SVG](w, "icon")
	layout.New(layout.FakeMeasurer{}).Layout(w.tree.Root, 200, 200)
	if b := n.Base().Bounds(); b.W != 10 || b.H != 20 {
		t.Fatalf("bounds = %vx%v, want 10x20", b.W, b.H)
	}
}

func svgPixel(n *SVG, x, y int) (r, g, b, a uint8) {
	i := (y*n.Data.W + x) * 4
	return n.Data.Pix[i], n.Data.Pix[i+1], n.Data.Pix[i+2], n.Data.Pix[i+3]
}
