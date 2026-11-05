package ui

import (
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

// TestSVGRejectsRemoteSrc keeps the loader honest: unlike Image, SVG reads a
// local file only, and says so instead of silently drawing nothing.
func TestSVGRejectsRemoteSrc(t *testing.T) {
	_, w := svgApp(t, `Window {
		Column {
			SVG { id: icon src: "https://example.invalid/icon.svg" width: 10 height: 10 }
		}
	}`)
	n := MustByID[*SVG](w, "icon")
	if n.LoadErr == nil {
		t.Fatal("a remote src was accepted")
	}
	if n.Data != nil {
		t.Fatal("a remote src produced a bitmap")
	}
}

func svgPixel(n *SVG, x, y int) (r, g, b, a uint8) {
	i := (y*n.Data.W + x) * 4
	return n.Data.Pix[i], n.Data.Pix[i+1], n.Data.Pix[i+2], n.Data.Pix[i+3]
}
