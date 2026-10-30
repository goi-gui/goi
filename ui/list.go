package ui

import (
	"fmt"

	"github.com/ksckaan1/goi/internal/tree"
)

// Instantiate copies a hidden template element under parent, which is how an app
// grows a list without building nodes by hand:
//
//	row, err := ui.Instantiate[*ui.ListItem](w, "rowTemplate", list)
//	row.Headline.Set("Ada")
//
// The template stays in the .goi file with `visible: false`; every copy is
// visible. Ids inside the copy are kept so Descendant can find them, but they
// are not registered, so copies never collide with each other or the template.
func Instantiate[T Node](w *Window, templateID string, parent Node) (T, error) {
	var zero T
	if w == nil || w.tree == nil {
		return zero, fmt.Errorf("ui: no window")
	}
	tpl, ok := w.tree.ByID(templateID)
	if !ok {
		return zero, fmt.Errorf("ui: no element with id %q", templateID)
	}
	if parent == nil {
		return zero, fmt.Errorf("ui: instantiate needs a parent")
	}
	n, err := w.tree.Instantiate(tpl, parent)
	if err != nil {
		return zero, err
	}
	t, ok := n.(T)
	if !ok {
		return zero, fmt.Errorf("ui: template %q is %s, not %T", templateID, n.Kind(), zero)
	}
	return t, nil
}

// Descendant finds an element by its .goi id inside a subtree, root included.
// Use it to reach the named parts of an Instantiate copy.
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

// Remove detaches a node from the tree, as the inverse of Instantiate.
func Remove(w *Window, n Node) {
	if w == nil || w.tree == nil || n == nil {
		return
	}
	w.tree.Remove(n)
}
