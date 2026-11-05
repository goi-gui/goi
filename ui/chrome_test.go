package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
)

// TestChromeOfReadsTheDesign pins the mapping from the root's properties to the
// platform state: the "auto" (-1) maximum becomes 0, which is what SDL reads as
// "no limit".
func TestChromeOfReadsTheDesign(t *testing.T) {
	_, w := reloadApp(t, `Window {
		minWidth: 400
		minHeight: 300
		maxWidth: 900
		disableResize: true
		fullscreen: true
	}`)
	got := chromeOf(w)
	want := chromeState{disableResize: true, fullscreen: true, minW: 400, minH: 300, maxW: 900, maxH: 0}
	if got != want {
		t.Fatalf("chrome = %+v, want %+v", got, want)
	}
}

// TestChromeFollowsGo keeps the frame loop's promise: setting a window property
// from Go marks the state dirty without a reload.
func TestChromeFollowsGo(t *testing.T) {
	a, w := reloadApp(t, `Window { minWidth: 320 }`)
	a.chrome = chromeOf(w)

	w.Root().DisableResize.Set(true)
	w.Root().Fullscreen.Set(true)

	if chromeOf(w) == a.chrome {
		t.Fatal("a Go change did not reach the window state")
	}
}

// TestPointsFloorsAuto guards the conversion: a negative length means "no
// limit", not a negative window size.
func TestPointsFloorsAuto(t *testing.T) {
	for in, want := range map[float64]int{-1: 0, 0: 0, 24: 24, 24.6: 25} {
		if got := points(in); got != want {
			t.Errorf("points(%v) = %d, want %d", in, got, want)
		}
	}
}

// TestChromeSettersReachThePlatform is the Go-facing half: every window chrome
// property must be writable from Go, not only from the design file.
func TestChromeSettersReachThePlatform(t *testing.T) {
	a, w := reloadApp(t, `Window { }`)
	a.chrome = chromeOf(w)
	r := w.Root()

	r.SetMinWidth(480)
	r.SetMinHeight(320)
	r.SetMaxWidth(1280)
	r.SetMaxHeight(720)
	r.DisableResize.Set(true)
	r.Fullscreen.Set(true)

	want := chromeState{disableResize: true, fullscreen: true, minW: 480, minH: 320, maxW: 1280, maxH: 720}
	if got := chromeOf(w); got != want {
		t.Fatalf("chrome = %+v, want %+v", got, want)
	}

	// The same setters clear a limit, which is what "no limit" means.
	r.SetMaxWidth(-1)
	r.SetMinWidth(-1)
	if got := chromeOf(w); got.maxW != 0 || got.minW != 0 {
		t.Fatalf("cleared limits = %+v", got)
	}
}

// TestWindowMinHeightLayouts keeps minHeight from being a window-only idea: it
// is a common property, so a child can ask for it too.
func TestWindowMinHeightLayouts(t *testing.T) {
	_, w := reloadApp(t, `Window {
		Column {
			Rectangle { id: box minHeight: 90 height: 20 }
		}
	}`)
	box := MustByID[*Rectangle](w, "box")
	layout.New(layout.FakeMeasurer{}).Layout(w.tree.Root, 400, 400)
	if got := box.Base().Bounds().H; got != 90 {
		t.Fatalf("height = %v, want the minHeight floor", got)
	}
}
