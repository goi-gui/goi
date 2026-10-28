package ui

import (
	"fmt"

	"github.com/ksckaan1/goui/internal/tree"
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
}

// OnKey subscribes to key presses in this window, whether or not a text field
// has focus. A drawer or a dialog uses it to close on Escape.
func (w *Window) OnKey(fn func(KeyEvent)) (off func()) { return w.keys.On(fn) }

// T returns the underlying tree. Internal use and tests.
func (w *Window) T() *tree.Tree { return w.tree }

// Root returns the root node.
func (w *Window) Root() *WindowNode { return w.tree.Root }

// Title returns the window title from the .ui file.
func (w *Window) Title() string { return w.title }

// Size returns the client size in logical pixels.
func (w *Window) Size() (int, int) { return w.width, w.height }

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
