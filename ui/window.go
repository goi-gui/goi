package ui

import (
	"fmt"
	"sync"

	"github.com/ksckaan1/goi/internal/platform"
	"github.com/ksckaan1/goi/internal/tree"
)

// Window wraps a live node tree. Its methods must only be called from the UI
// goroutine (inside Bind or via App.Post).
type Window struct {
	tree    *tree.Tree
	app     *App
	title   string
	width   int
	height  int
	binding func(*Window) error
	keys    tree.Event[tree.KeyEvent]

	// customColors holds the names the app defined with SetCustomColor, and
	// missingColors the names a design used that nobody defined, so the warning
	// is logged once. Both are guarded because SetCustomColor may be called from
	// any goroutine while a frame resolves colours.
	colorMu       sync.RWMutex
	customColors  map[string]uint32
	missingColors map[string]bool
}

// OnKey subscribes to key presses in this window, whether or not a text field
// has focus. A drawer or a dialog uses it to close on Escape.
func (w *Window) OnKey(fn func(KeyEvent)) (off func()) { return w.keys.On(fn) }

// T returns the underlying tree. Internal use and tests.
func (w *Window) T() *tree.Tree { return w.tree }

// Root returns the root node.
func (w *Window) Root() *WindowNode { return w.tree.Root }

// Title returns the window title from the .goi file.
func (w *Window) Title() string { return w.title }

// Size returns the client size in logical pixels.
func (w *Window) Size() (int, int) { return w.width, w.height }

// native returns the live platform window, or nil in headless tests.
func (w *Window) native() *platform.Window {
	if w.app == nil {
		return nil
	}
	d, _ := w.app.driver.(*gpuDriver)
	if d == nil {
		return nil
	}
	return d.win
}

// Minimize hides the window in the task bar.
func (w *Window) Minimize() {
	if p := w.native(); p != nil {
		p.Minimize()
	}
}

// ToggleMaximize maximizes the window, or restores it when already maximized.
func (w *Window) ToggleMaximize() {
	if p := w.native(); p != nil {
		p.ToggleMaximize()
	}
}

// Maximized reports whether the window is maximized. A headless window is
// never maximized.
func (w *Window) Maximized() bool {
	p := w.native()
	return p != nil && p.Maximized()
}

// Close asks the window to close, as the platform's close button would. The
// event loop returns after the current frame.
func (w *Window) Close() {
	if p := w.native(); p != nil {
		p.Close()
	}
}

// ByID returns the node with the given id, or an error naming the id.
func (w *Window) ByID(id string) (Node, error) {
	n, ok := w.tree.ByID(id)
	if !ok {
		return nil, fmt.Errorf("ui: no element with id %q", id)
	}
	return n, nil
}

// Tree returns the live tree for tests and debugging.
func (w *Window) Tree() *tree.Tree { return w.tree }

// Mode reports whether the window currently shows a light or a dark scheme.
// It follows the resolved `theme`, so a registered dark theme reports ModeDark
// too. The design side reads the same value as `app.mode`.
//
//	switch w.Mode() {
//	case ui.ModeDark:
//		icon.Icon.Set("dark_mode")
//	}
func (w *Window) Mode() Mode {
	if w == nil || w.tree == nil {
		return ModeLight
	}
	return w.tree.Mode()
}

// SystemMode reports the desktop's light/dark preference, or ModeUnknown when
// the platform does not report one. A design can follow it without any Go code
// by naming it as the theme:
//
//	Window { theme: app.systemMode }
func (w *Window) SystemMode() Mode {
	if w == nil || w.tree == nil {
		return ModeUnknown
	}
	return w.tree.SystemMode()
}

// ByID returns the node with the given id, typed. The id must exist and must
// name a node of type T (for example *ui.Button):
//
//	btn, err := ui.ByID[*ui.Button](w, "inc")
//
// A missing id or a type mismatch returns an error explaining which.
func ByID[T Node](w *Window, id string) (T, error) {
	var zero T
	n, ok := w.tree.ByID(id)
	if !ok {
		return zero, fmt.Errorf("ui: no element with id %q", id)
	}
	t, ok := n.(T)
	if !ok {
		return zero, fmt.Errorf("ui: element %q is %s, not %T", id, n.Kind(), zero)
	}
	return t, nil
}

// MustByID is ByID but panics. Use it inside Bind where a missing id is a
// programming error rather than a runtime condition.
func MustByID[T Node](w *Window, id string) T {
	n, err := ByID[T](w, id)
	if err != nil {
		panic(err)
	}
	return n
}
