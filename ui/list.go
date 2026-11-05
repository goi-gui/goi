package ui

import (
	"fmt"

	"github.com/goi-gui/goi/internal/tree"
)

// Instantiate copies a hidden template element under parent, which is how an app
// grows a list without building nodes by hand:
//
//	row, err := ui.Instantiate[*ui.ListItem](w, "rowTemplate", list)
//	row.Headline.Set("Ada")
//
// The template stays in the .goi file, usually as `template: true`; every copy
// is visible. Ids inside the copy are kept so Descendant can find them, but they
// are not registered, so copies never collide with each other or the template.
// For a whole list, Fill does the clearing and cloning in one call.
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

// Fill rebuilds parent from its template child: every other child is removed,
// one copy of the template is instantiated per item, and fill writes the item
// into the copy. The template is the child whose design says `template: true`;
// it stays hidden and in place, so calling Fill again (a refresh, or Bind after
// a hot reload) replaces the rows instead of stacking them:
//
//	ui.Fill(w, list, items, func(row *ui.ListItem, it item) {
//		row.Label.Set(it.Title)
//	})
//
// The copies are ordinary rows, not templates, so a second Fill starts from the
// same prototype.
func Fill[T Node, I any](w *Window, parent Node, items []I, fill func(T, I)) error {
	var zero T
	if w == nil || w.tree == nil {
		return fmt.Errorf("ui: no window")
	}
	if parent == nil {
		return fmt.Errorf("ui: fill needs a parent")
	}
	var tpl Node
	for _, c := range parent.Base().Children() {
		if !c.Base().Template() {
			continue
		}
		if tpl != nil {
			return fmt.Errorf("ui: %s has more than one template child", parent.Kind())
		}
		tpl = c
	}
	if tpl == nil {
		return fmt.Errorf("ui: %s has no template child: mark the row element `template: true`", parent.Kind())
	}
	// Clear first, then build: the container ends up holding exactly one copy
	// per item plus the hidden prototype.
	for _, c := range parent.Base().Children() {
		if c != tpl {
			w.tree.Remove(c)
		}
	}
	for _, it := range items {
		n, err := w.tree.Instantiate(tpl, parent)
		if err != nil {
			return err
		}
		row, ok := n.(T)
		if !ok {
			return fmt.Errorf("ui: template is %s, not %T", n.Kind(), zero)
		}
		if fill != nil {
			fill(row, it)
		}
	}
	return nil
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

// SetData sets an app value a design reads as `data.name`. A list variable
// rebuilds every {#each} that walks it, and any condition or expression reading
// the value updates on the next frame, so a fetch only has to post this call.
//
//	ui.SetData(w, "loading", true)
//	ui.SetData(w, "tickets", items)
//
// A list element may be a struct, whose fields are matched by `json` tag and
// then by name, or a map[string]any. Call it from Bind or App.Post: it touches
// the tree.
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
