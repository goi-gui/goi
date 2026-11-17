package ui

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
)

// lazyDesign puts one element below a tall spacer in a scrolling page, so it
// starts outside the viewport and can be scrolled in.
func lazyDesign(el string) string {
	return `Window {
		Column {
			id:     page
			scroll: true
			Rectangle { width: 100 height: 500 }
			` + el + `
		}
	}`
}

// TestLazyImageWaitsForTheViewport: a lazy image does not fetch while it is
// below the page, and starts when the scroll brings it in. The box already
// exists, which is what the design's width and height are for.
func TestLazyImageWaitsForTheViewport(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(testPNG(t))
	}))
	defer srv.Close()

	a, win, _ := inputFixture(t, lazyDesign(`Image {
		id:    pic
		lazy:  true
		src:   "`+srv.URL+`/a.png"
		width:  40
		height: 20
	}`), 300, 100)
	im := MustByID[*Image](win, "pic")
	a.loadImages(win.tree)

	if im.RenderedSrc != "" || im.Loading || hits.Load() != 0 {
		t.Fatalf("an off-screen lazy image started: src=%q loading=%v hits=%d", im.RenderedSrc, im.Loading, hits.Load())
	}

	page := MustByID[*Column](win, "page")
	page.Base().SetScrollOffset(10_000)
	layout.New(textMeasurer{}).Layout(win.tree.Root, 300, 100)
	a.syncImages()
	if im.RenderedSrc != srv.URL+"/a.png" {
		t.Fatalf("the visible image did not start loading: %q", im.RenderedSrc)
	}
	pumpPosted(t, a, func() bool { return im.Data != nil })
	if hits.Load() != 1 {
		t.Fatalf("requests = %d, want one", hits.Load())
	}
}

// TestLazySVGWaitsForTheViewport is the same rule for a vector file.
func TestLazySVGWaitsForTheViewport(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="black"/></svg>`))
	}))
	defer srv.Close()

	a, win, _ := inputFixture(t, lazyDesign(`SVG {
		id:    mark
		lazy:  true
		src:   "`+srv.URL+`/mark.svg"
		width:  20
		height: 20
	}`), 300, 100)
	s := MustByID[*SVG](win, "mark")
	a.loadImages(win.tree)

	if s.RenderedSrc != "" || s.Loading || hits.Load() != 0 {
		t.Fatalf("an off-screen lazy SVG started: src=%q loading=%v hits=%d", s.RenderedSrc, s.Loading, hits.Load())
	}

	page := MustByID[*Column](win, "page")
	page.Base().SetScrollOffset(10_000)
	layout.New(textMeasurer{}).Layout(win.tree.Root, 300, 100)
	a.syncSVGs()
	if s.RenderedSrc != srv.URL+"/mark.svg" {
		t.Fatalf("the visible SVG did not start loading: %q", s.RenderedSrc)
	}
	pumpPosted(t, a, func() bool { return s.Data != nil })
	if hits.Load() != 1 {
		t.Fatalf("requests = %d, want one", hits.Load())
	}
}

// TestLazyImageLoadsWithoutADeclaredSize: a box whose size comes from the file
// cannot wait for a viewport it does not have yet, so it loads at once instead
// of staying a zero-sized hole forever.
func TestLazyImageLoadsWithoutADeclaredSize(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(testPNG(t))
	}))
	defer srv.Close()

	a, win, _ := inputFixture(t, `Window {
		Column {
			Image { id: pic lazy: true src: "`+srv.URL+`/a.png" }
		}
	}`, 300, 100)
	im := MustByID[*Image](win, "pic")
	a.loadImages(win.tree)
	a.syncImages()
	pumpPosted(t, a, func() bool { return im.Data != nil })
	if hits.Load() != 1 {
		t.Fatalf("requests = %d, want one for a sized-by-content image", hits.Load())
	}
}
