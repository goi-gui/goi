package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/goi-gui/goi/internal/tree"
)

// testPNG is a 3x2 image, small enough to inline and big enough that a decode
// mix-up shows in the dimensions.
func testPNG(t *testing.T) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			m.Set(x, y, color.RGBA{R: 0x20, G: 0x80, B: 0xC0, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pumpPosted runs queued work until cond is true. The fetch posts its result to
// the UI goroutine, and a headless app queues those instead of waking a driver.
func pumpPosted(t *testing.T, a *App, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.drainPosted()
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the load to finish")
}

// TestRemoteImageLoads covers the whole path: a design that points at an http
// URL shows the placeholder first, and the decoded bitmap lands without a
// reload.
func TestRemoteImageLoads(t *testing.T) {
	png := testPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "image/*" {
			t.Errorf("Accept = %q, want image/*", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	}))
	defer srv.Close()

	a, win := reloadApp(t, `Window { Column { Image { id: pic src: "`+srv.URL+`/a.png" width: 40 height: 20 } } }`)
	im := MustByID[*Image](win, "pic")
	a.loadImages(win.tree)

	// The window can paint at once: no bitmap yet, but the node is loading.
	if im.Data != nil {
		t.Fatal("the bitmap was ready synchronously")
	}
	if !im.Loading {
		t.Fatal("a remote src did not mark the node as loading")
	}
	if !a.pulsing {
		t.Fatal("the placeholder was not started")
	}

	pumpPosted(t, a, func() bool { return im.Data != nil })
	if im.Loading {
		t.Fatal("still loading after the bitmap arrived")
	}
	if im.Data.W != 3 || im.Data.H != 2 {
		t.Fatalf("decoded %dx%d, want 3x2", im.Data.W, im.Data.H)
	}
	if im.LoadErr != nil {
		t.Fatalf("LoadErr = %v", im.LoadErr)
	}
}

// TestRemoteImageFailureKeepsThePlaceholder checks the failure path: the node
// stops loading, reports why, and keeps drawing its background.
func TestRemoteImageFailureKeepsThePlaceholder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	a, win := reloadApp(t, `Window { Column { Image { id: pic src: "`+srv.URL+`/gone.png" width: 40 height: 20 } } }`)
	im := MustByID[*Image](win, "pic")
	a.loadImages(win.tree)

	pumpPosted(t, a, func() bool { return !im.Loading })
	if im.Data != nil {
		t.Fatal("a 404 produced a bitmap")
	}
	if im.LoadErr == nil {
		t.Fatal("the failure was not recorded")
	}
}

// TestRemoteImageIsSharedAndCached keeps a reload from fetching the same URL
// twice: two nodes with one src make one request, and the cache answers later
// trees.
func TestRemoteImageIsSharedAndCached(t *testing.T) {
	png := testPNG(t)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write(png)
	}))
	defer srv.Close()

	src := srv.URL + "/shared.png"
	a, win := reloadApp(t, `Window { Column {
		Image { id: one src: "`+src+`" width: 40 height: 20 }
		Image { id: two src: "`+src+`" width: 40 height: 20 }
	} }`)
	one := MustByID[*Image](win, "one")
	two := MustByID[*Image](win, "two")
	a.loadImages(win.tree)
	pumpPosted(t, a, func() bool { return one.Data != nil && two.Data != nil })

	if hits != 1 {
		t.Fatalf("%d requests for one URL, want 1", hits)
	}

	// A second tree with the same src takes the cached bitmap straight away.
	_, win2 := reloadApp(t, `Window { Column { Image { id: three src: "`+src+`" width: 40 height: 20 } } }`)
	three := MustByID[*Image](win2, "three")
	a.loadImages(win2.tree)
	if three.Data == nil || three.Loading {
		t.Fatal("the cache did not answer the reload")
	}
	if hits != 1 {
		t.Fatalf("%d requests after the cache hit, want 1", hits)
	}
}

// TestLocalImageNeedsNoHTTP guards the other branch: a path is read from disk
// and never treated as a URL.
func TestLocalImageNeedsNoHTTP(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()

	a, win := reloadApp(t, `Window { Column { Image { id: pic src: "httpthing.png" width: 40 height: 20 } } }`)
	im := MustByID[*Image](win, "pic")
	a.loadImages(win.tree)
	if im.Loading {
		t.Fatal("a local path was treated as a URL")
	}
	if hits != 0 {
		t.Fatal("a local path made a request")
	}
}

// TestImageLoadingPulsesAndStops covers the frame loop side: the placeholder asks
// for frames while loading and stops by itself once nothing is left.
func TestImageLoadingPulsesAndStops(t *testing.T) {
	a, win := reloadApp(t, `Window { Column { Image { id: pic src: "https://example.invalid/a.png" width: 40 height: 20 } } }`)
	im := MustByID[*Image](win, "pic")
	im.Loading = true
	a.pulsing = true

	if !a.tickImageLoads(1.0 / 60) {
		t.Fatal("a loading image did not ask for a frame")
	}
	first := im.Phase
	a.tickImageLoads(0.3)
	if im.Phase == first {
		t.Fatalf("phase did not advance: %v", im.Phase)
	}
	if im.Phase < 0 || im.Phase > 1 {
		t.Fatalf("phase = %v, want 0..1", im.Phase)
	}

	im.Loading = false
	if a.tickImageLoads(1.0 / 60) {
		t.Fatal("the pulse kept going after the load finished")
	}
	if a.pulsing {
		t.Fatal("the pulse flag stayed on")
	}
}

// testGIF builds a two-frame animation, red then blue, 50ms each.
func testGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{R: 0xFF, A: 0xFF}, color.RGBA{B: 0xFF, A: 0xFF}}
	frames := make([]*image.Paletted, 2)
	for i := range frames {
		frames[i] = image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
		for j := range frames[i].Pix {
			frames[i].Pix[j] = uint8(i)
		}
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image:  frames,
		Delay:  []int{5, 5},
		Config: image.Config{Width: 4, Height: 4, ColorModel: pal},
	}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestImageAnimatedGIF loads a GIF from the asset container and plays it: the
// node gets the animation, and a tick swaps the pixels in place.
func TestImageAnimatedGIF(t *testing.T) {
	a := newHeadlessApp()
	a.assets = fstest.MapFS{"spin.gif": &fstest.MapFile{Data: testGIF(t)}}
	a.win = &Window{}
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Image { id: spin src: "goi://spin.gif" width: 40 height: 40 }
	} }`), &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	a.win.tree = tr
	a.loadImages(tr)

	im := MustByID[*Image](a.win, "spin")
	if im.Anim == nil {
		t.Fatal("the GIF loaded as a still image")
	}
	if im.Data == nil || im.Data.W != 4 || im.Frame != 0 {
		t.Fatalf("Data = %+v, frame = %d", im.Data, im.Frame)
	}
	if im.Data.Pix[0] != 0xFF || im.Data.Pix[2] != 0 {
		t.Fatalf("frame 0 = %v, want red", im.Data.Pix[:4])
	}
	gen := im.Data.Gen

	// Past the first frame's 50ms the player swaps Pix and bumps the generation
	// the renderer watches.
	if !a.tickAnimatedImages(0.06) {
		t.Fatal("an animated image did not ask for another frame")
	}
	if im.Frame != 1 || im.Data.Pix[2] != 0xFF {
		t.Fatalf("frame = %d, pix = %v, want the blue frame", im.Frame, im.Data.Pix[:4])
	}
	if im.Data.Gen == gen {
		t.Fatal("the pixel swap did not bump the upload generation")
	}

	// Paused stops the clock.
	im.Paused.Set(true)
	im.AnimTime = 0
	a.hasAnim = true
	if a.tickAnimatedImages(0.06) {
		t.Fatal("a paused animation asked for a frame")
	}
	if im.Frame != 1 {
		t.Fatalf("frame = %d, want it held while paused", im.Frame)
	}
}

// TestImageSetImageClearsAnim keeps the Go path still: a bitmap handed to the
// node replaces the animation it was playing.
func TestImageSetImageClearsAnim(t *testing.T) {
	a := newHeadlessApp()
	a.assets = fstest.MapFS{"spin.gif": &fstest.MapFile{Data: testGIF(t)}}
	a.win = &Window{}
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Image { id: spin src: "goi://spin.gif" }
	} }`), &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	a.win.tree = tr
	a.loadImages(tr)
	im := MustByID[*Image](a.win, "spin")
	if im.Anim == nil {
		t.Fatal("no animation to replace")
	}
	im.SetImage(image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if im.Anim != nil || im.Paused.Get() {
		t.Fatalf("SetImage left the animation: anim=%v paused=%v", im.Anim, im.Paused)
	}
}

// TestImageAnimationsAreIndependent keeps two elements with the same src on
// their own frame, so one pause or clock does not move the other.
func TestImageAnimationsAreIndependent(t *testing.T) {
	a := newHeadlessApp()
	a.assets = fstest.MapFS{"spin.gif": &fstest.MapFile{Data: testGIF(t)}}
	a.win = &Window{}
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Image { id: one src: "goi://spin.gif" }
		Image { id: two src: "goi://spin.gif" }
	} }`), &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	a.win.tree = tr
	a.loadImages(tr)
	one := MustByID[*Image](a.win, "one")
	two := MustByID[*Image](a.win, "two")
	if one.Data == two.Data {
		t.Fatal("two animated nodes share one bitmap, so a frame swap moves both")
	}
	two.Paused.Set(true)
	a.tickAnimatedImages(0.06)
	if one.Frame != 1 {
		t.Fatalf("frame = %d, want the first image to advance", one.Frame)
	}
	if two.Frame != 0 {
		t.Fatalf("paused frame = %d, want it held", two.Frame)
	}
}

// animSVGFixture is a 10x10 rect whose fill cycles red to blue once a second.
const animSVGFixture = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
	<rect width="10" height="10" fill="#ff0000">
		<animate attributeName="fill" from="#ff0000" to="#0000ff" dur="1s" repeatCount="indefinite"/>
	</rect>
</svg>`

// TestAnimatedSVGPlays checks that an SVG with a SMIL animation keeps the frame
// loop awake and re-rasterizes into the node's own bitmap.
func TestAnimatedSVGPlays(t *testing.T) {
	a := newHeadlessApp()
	assets := fstest.MapFS{"spin.svg": &fstest.MapFile{Data: []byte(animSVGFixture)},
		"plain.svg": &fstest.MapFile{Data: []byte(testSVGPlain)}}
	a.assets = assets
	a.win = &Window{}
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		SVG { id: spin src: "goi://spin.svg" width: 20 height: 20 }
	} }`), &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	a.win.tree = tr
	a.loadImages(tr)
	n := MustByID[*SVG](a.win, "spin")
	if !n.Source.Animated() || n.AnimDur != time.Second {
		t.Fatalf("Source = %v, AnimDur = %v", n.Source, n.AnimDur)
	}
	first := n.Data
	if first == nil {
		t.Fatal("the animated SVG drew nothing")
	}
	gen := first.Gen

	if !a.tickAnimatedSVGs(0.2) {
		t.Fatal("an animated SVG did not ask for another frame")
	}
	if n.Data == first {
		t.Fatal("the SVG was not re-rasterized")
	}
	if n.Data.Gen == gen {
		t.Fatal("the new frame did not bump the upload generation")
	}

	// paused freezes the clock.
	n.Paused.Set(true)
	before := n.Frame
	a.tickAnimatedSVGs(0.2)
	if n.Frame != before {
		t.Fatalf("frame moved while paused: %d -> %d", before, n.Frame)
	}

	// A still document never animates.
	tr2, diag := tree.BuildSource([]byte(`Window { Column {
		SVG { id: plain src: "goi://plain.svg" width: 20 height: 20 }
	} }`), &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	a.win.tree = tr2
	a.loadImages(tr2)
	if p := MustByID[*SVG](a.win, "plain"); p.AnimDur != 0 || p.Source != nil {
		t.Fatalf("a still SVG reported an animation: %v %v", p.Source, p.AnimDur)
	}
	if a.tickAnimatedSVGs(0.2) {
		t.Fatal("a still SVG asked for another frame")
	}
}

// testSVGPlain is a still document, used to keep the animated path honest.
const testSVGPlain = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
	<rect width="10" height="10" fill="#00ff00"/>
</svg>`

// TestPausedPropertyIsDeclarative checks `paused: true` in a design file, which
// is what a still screenshot of an animation needs.
func TestPausedPropertyIsDeclarative(t *testing.T) {
	a := newHeadlessApp()
	a.assets = fstest.MapFS{"spin.gif": &fstest.MapFile{Data: testGIF(t)}}
	a.win = &Window{}
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Image { id: spin src: "goi://spin.gif" paused: true }
	} }`), &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	a.win.tree = tr
	a.loadImages(tr)
	im := MustByID[*Image](a.win, "spin")
	if !im.Paused.Get() {
		t.Fatal("paused: true did not reach the node")
	}
	if a.tickAnimatedImages(0.06) {
		t.Fatal("a paused animation asked for a frame")
	}
	if im.Frame != 0 {
		t.Fatalf("frame = %d, want it held at the first", im.Frame)
	}
}
