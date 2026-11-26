package ui

import (
	"fmt"

	"github.com/goi-gui/goi/internal/tree"
)

// Descendant finds an element by its .goi id inside a subtree, root included.
// It reaches the scoped ids of a {#each} row, which Tree.ByID does not index.
func Descendant[T Node](root Node, id string) (T, error) {
	var zero T
	if root == nil || root.Base().TreeRef() == nil {
		return zero, fmt.Errorf("ui: no element with id %q", id)
	}
	n, ok := tree.Find(root, id)
	if !ok {
		return zero, fmt.Errorf("ui: no element with id %q", id)
	}
	t, ok := n.(T)
	if !ok {
		return zero, fmt.Errorf("ui: element %q is %s, not %T", id, n.Kind(), zero)
	}
	return t, nil
}

// Remove detaches a node from the tree, including its subtree. A list built by
// a {#each} loop is rebuilt by SetData; this is for a node that should simply
// go away.
func Remove(w *Window, n Node) {
	if w == nil || w.tree == nil || n == nil {
		return
	}
	w.tree.Remove(n)
}

// SetData sets an app value a design reads as `data.name`. A list variable
// rebuilds every {#each} that walks it, and any condition or expression reading
// the value updates on the next frame, so a fetch only has to post this call.
//
//	ui.SetData(w, "loading", true)
//	ui.SetData(w, "tickets", items)
//
// A list element may be a struct, whose fields are matched by `json` tag and
// then by name, or a map[string]any. Call it from Bind or App.Post: it touches
// the tree. Hot reload carries the stored values into the new tree.
func SetData(w *Window, name string, v any) {
	if w == nil || w.tree == nil {
		return
	}
	w.tree.SetData(name, v)
	if err := w.tree.Err(); err != nil {
		w.app.reportError(err)
	}
	w.app.markDirty(tree.DirtyLayout)
}

// OnEach runs fn for every row a {#each} builds from `data.name`, with the row's
// first element and the item itself. It is how a row gets a click handler:
//
//	ui.OnEach[*ui.ListItem](w, "tickets", func(row *ui.ListItem, it ticket) {
//		row.OnClick(func(ui.ClickEvent) { open(it) })
//	})
//
// The hook runs again for every rebuilt row, so it must be safe to call more
// than once. Rows that already exist are hooked when the call is made.
func OnEach[T Node, I any](w *Window, data string, fn func(T, I)) {
	if w == nil || w.tree == nil || fn == nil {
		return
	}
	w.tree.OnEach(data, func(n Node, item any) {
		row, ok := n.(T)
		if !ok {
			return
		}
		it, ok := item.(I)
		if !ok {
			return
		}
		fn(row, it)
	})
}
