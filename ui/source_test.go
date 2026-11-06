package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/goi-gui/goi/internal/tree"
)

// TestReadSrcSchemes pins the three local forms: `goi://` is the design's
// container, a plain path is the working directory, and an absolute path is
// itself.
func TestReadSrcSchemes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "disk.png"), []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(dir, "disk.png")

	a := newHeadlessApp()
	a.assets = fstest.MapFS{"in-fs.png": &fstest.MapFile{Data: []byte("asset")}}

	if b, err := a.readSrc("goi://in-fs.png"); err != nil || string(b) != "asset" {
		t.Fatalf("goi:// = %q, %v", b, err)
	}
	if b, err := a.readSrc(abs); err != nil || string(b) != "disk" {
		t.Fatalf("absolute = %q, %v", b, err)
	}

	// A relative path is the working directory, not the container.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := os.WriteFile("rel.png", []byte("rel"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { t.Chdir(wd) })
	if b, err := a.readSrc("rel.png"); err != nil || string(b) != "rel" {
		t.Fatalf("relative = %q, %v", b, err)
	}

	// `goi://` without a container is a clear error, not a silent skip.
	empty := newHeadlessApp()
	if _, err := empty.readSrc("goi://x.png"); err == nil {
		t.Fatal("an asset src without a container was accepted")
	}
}

// TestImageFromAssetAndDisk covers the two local branches end to end: one design
// reads the container, the other a file on disk.
func TestImageFromAssetAndDisk(t *testing.T) {
	pngBytes := testPNG(t)
	dir := t.TempDir()
	diskPath := filepath.Join(dir, "disk.png")
	if err := os.WriteFile(diskPath, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	a := newHeadlessApp()
	a.assets = fstest.MapFS{"box.png": &fstest.MapFile{Data: pngBytes}}
	a.win = &Window{}
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Image { id: box src: "goi://box.png" width: 10 height: 10 }
		Image { id: disk src: "`+diskPath+`" width: 10 height: 10 }
	} }`), &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	a.win.tree = tr
	a.loadImages(tr)

	box := MustByID[*Image](a.win, "box")
	disk := MustByID[*Image](a.win, "disk")
	if box.Data == nil || box.LoadErr != nil {
		t.Fatalf("container image: %v", box.LoadErr)
	}
	if disk.Data == nil || disk.LoadErr != nil {
		t.Fatalf("disk image: %v", disk.LoadErr)
	}
	if box.Data.W != 3 || disk.Data.W != 3 {
		t.Fatalf("decoded %dx%d and %dx%d, want 3 wide", box.Data.W, box.Data.H, disk.Data.W, disk.Data.H)
	}
}

// TestImageSetImage is the Go-only path: an image that exists in memory needs no
// file at all.
func TestImageSetImage(t *testing.T) {
	_, w := reloadApp(t, `Window { Column { Image { id: pic width: 40 height: 20 } } }`)
	im := MustByID[*Image](w, "pic")
	if im.Data != nil {
		t.Fatal("a fresh node already has a bitmap")
	}

	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	src.Set(0, 0, color.RGBA{R: 0xFF, A: 0xFF})
	im.SetImage(src)

	if im.Data == nil || im.Data.W != 4 || im.Data.H != 2 {
		t.Fatalf("Data = %+v", im.Data)
	}
	if _, dirty := im.Base().Dirty(); !dirty {
		t.Fatal("SetImage did not mark the node for repaint")
	}

	im.SetImage(nil)
	if im.Data != nil {
		t.Fatal("a nil image did not clear the bitmap")
	}
}

// TestSVGSetData covers both setters: a document from Go draws without a file,
// and a new set re-rasterizes.
func TestSVGSetData(t *testing.T) {
	a, w := reloadApp(t, `Window { Column { SVG { id: icon color: #ff0000 } } }`)
	n := MustByID[*SVG](w, "icon")

	n.SetDataString(svgFixture)
	if n.InlineGen != 1 {
		t.Fatalf("InlineGen = %d", n.InlineGen)
	}
	a.syncSVGs()
	if n.LoadErr != nil {
		t.Fatalf("LoadErr = %v", n.LoadErr)
	}
	if n.Data == nil || n.IntrinsicW != 10 || n.IntrinsicH != 20 {
		t.Fatalf("Data = %+v, intrinsic %vx%v", n.Data, n.IntrinsicW, n.IntrinsicH)
	}
	first := n.Data

	n.SetDataBytes([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 5 5">
		<rect width="5" height="5" fill="currentColor"/>
	</svg>`))
	a.syncSVGs()
	if n.Data == first {
		t.Fatal("a new document reused the old raster")
	}
	if n.IntrinsicW != 5 || n.IntrinsicH != 5 {
		t.Fatalf("intrinsic = %vx%v, want 5x5", n.IntrinsicW, n.IntrinsicH)
	}
}

// TestSVGInlineWinsOverSrc keeps the rule from the docs: a document set from Go
// replaces `src` instead of being overwritten by it.
func TestSVGInlineWinsOverSrc(t *testing.T) {
	a, w := svgApp(t, `Window { Column {
		SVG { id: icon src: "goi://icon.svg" }
	} }`)
	n := MustByID[*SVG](w, "icon")
	if n.Data == nil {
		t.Fatal("the file did not load")
	}
	n.SetDataBytes([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 7 7">
		<rect width="7" height="7" fill="#00ff00"/>
	</svg>`))
	a.syncSVGs()
	if n.IntrinsicW != 7 {
		t.Fatalf("intrinsic = %v, the file won over the document", n.IntrinsicW)
	}
	if r, g, b, _ := svgPixel(n, n.Data.W/2, n.Data.H/2); g < 240 || r > 15 || b > 15 {
		t.Fatalf("centre = %d,%d,%d, want green", r, g, b)
	}
}

// TestPNGRoundTrip decodes the same fixture two ways: bytes for the renderer
// and an image.Image handed straight to SetImage.
func TestPNGRoundTrip(t *testing.T) {
	b := testPNG(t)
	src, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	_, w := reloadApp(t, `Window { Column { Image { id: pic } } }`)
	im := MustByID[*Image](w, "pic")
	im.SetImage(src)
	if im.Data.W != 3 || im.Data.H != 2 {
		t.Fatalf("Data = %dx%d", im.Data.W, im.Data.H)
	}
}
