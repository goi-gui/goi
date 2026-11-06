package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
