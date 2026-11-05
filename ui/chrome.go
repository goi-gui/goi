package ui

import (
	"github.com/ksckaan1/goi/internal/platform"
)

// chromeState is the Window's platform-side state: what the design asked of the
// native window rather than of the client area.
type chromeState struct {
	disableResize bool
	fullscreen    bool
	minW, minH    int
	maxW, maxH    int // 0 means unbounded
}

// chromeOf reads the root's window properties.
func chromeOf(w *Window) chromeState {
	r := w.tree.Root
	return chromeState{
		disableResize: r.DisableResize.Get(),
		fullscreen:    r.Fullscreen.Get(),
		minW:          points(r.MinWidth()),
		minH:          points(r.MinHeight()),
		maxW:          points(r.MaxWidth()),
		maxH:          points(r.MaxHeight()),
	}
}

// points converts a design length to a whole logical pixel count. A negative
// length (the "auto" default) becomes 0, which is what the platform reads as
// "no limit".
func points(v float64) int {
	if v <= 0 {
		return 0
	}
	return int(v + 0.5)
}

// applyChrome pushes st to the platform window. It is idempotent, so the frame
// loop can call it whenever the state difference is detected.
func (a *App) applyChrome(pw *platform.Window, st chromeState) {
	if pw == nil {
		return
	}
	pw.SetSizeLimits(st.minW, st.minH, st.maxW, st.maxH)
	pw.SetResizable(!st.disableResize)
	pw.SetFullscreen(st.fullscreen)
	// A borderless window resizes from its own edges, so that path follows the
	// same switch as the platform's title bar.
	if d, ok := a.driver.(*gpuDriver); ok && d.input != nil {
		d.input.resizeEdges = a.win.tree.Root.TitleBar.Get() == "custom" && !st.disableResize
	}
}

// syncChrome applies the window properties when they changed, which is what
// makes Set from Go or an expression take effect without a reload. It runs at
// most once per frame and costs a struct comparison when nothing moved.
func (a *App) syncChrome() {
	if a.win == nil || a.win.tree == nil || a.win.tree.Root == nil {
		return
	}
	d, ok := a.driver.(*gpuDriver)
	if !ok || d.win == nil {
		return
	}
	st := chromeOf(a.win)
	if st == a.chrome {
		return
	}
	a.chrome = st
	a.applyChrome(d.win, st)
}
