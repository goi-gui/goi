package ui

import (
	"fmt"

	"github.com/ksckaan1/goui/internal/hotreload"
	"github.com/ksckaan1/goui/internal/schema"
	"github.com/ksckaan1/goui/internal/syntax"
	"github.com/ksckaan1/goui/internal/tree"
)

// reloadSrc is the watcher callback. It runs on the watcher goroutine, so it
// parses and validates there and only the swap happens on the UI goroutine.
func (a *App) reloadSrc(src []byte) {
	path := a.path
	if src == nil {
		a.Post(func() { a.reportError(fmt.Errorf("goui: cannot read %s", path)) })
		return
	}
	f, diag := syntax.Parse(src)
	if !diag.Empty() {
		msg := diag.Format(path, src)
		a.Post(func() { a.reportError(fmt.Errorf("%s", msg)) })
		return
	}
	if _, diag := schema.Validate(f); !diag.Empty() {
		msg := diag.Format(path, src)
		a.Post(func() { a.reportError(fmt.Errorf("%s", msg)) })
		return
	}
	a.Post(func() {
		if err := a.applyReload(f); err != nil {
			a.reportError(err)
		}
	})
}

// applyReload swaps in a new tree, carrying over the state the framework owns.
// It runs on the UI goroutine.
//
// Handlers are never carried over: Bind runs again so the app can attach them
// to the new nodes. App state therefore has to live outside Bind.
func (a *App) applyReload(f *syntax.File) error {
	w := a.win
	if w == nil {
		return nil
	}
	old := w.tree

	// Remember focus by id so it can be re-resolved on the new tree.
	var focusID string
	if d, ok := a.driver.(*gpuDriver); ok && d.input != nil && d.input.focus != nil {
		focusID = d.input.focus.Base().ID()
	}

	tr, err := tree.Build(f, &tree.Hooks{Marked: a.markDirty})
	if err != nil {
		return fmt.Errorf("goui: reload: %w", err)
	}
	carryOver(old, tr)

	w.tree = tr
	w.title = tr.Root.Title.Get()
	if d, ok := a.driver.(*gpuDriver); ok {
		d.input.tree = tr
		d.input.focus = nil
		if focusID != "" {
			if n, ok := tr.ByID(focusID); ok {
				if ti, ok := n.(*tree.TextInput); ok {
					d.input.focus = ti
				}
			}
		}
	}

	if err := w.Bind(w.binding); err != nil {
		return fmt.Errorf("goui: reload: bind: %w", err)
	}
	// Everything must be repainted after a swap: the display list is rebuilt
	// from scratch and the old node pointers are gone.
	for _, n := range tr.All() {
		n.Base().MarkDirty(tree.DirtyLayout)
	}
	tr.Root.Base().MarkDirty(tree.DirtyLayout)
	a.needsFrame = true
	if a.driver != nil {
		a.driver.wake()
	}
	return nil
}

// carryOver copies widget state and Go-set property values from the old tree to
// the new one, keyed by id.
func carryOver(old, fresh *tree.Tree) {
	for _, n := range fresh.All() {
		id := n.Base().ID()
		if id == "" {
			continue
		}
		src, ok := old.ByID(id)
		if !ok || src.Kind() != n.Kind() {
			continue
		}
		sb, db := src.Base(), n.Base()

		// Property values Go wrote survive, unless the .ui file changed that
		// property's literal between the two versions.
		for _, name := range propNames(db) {
			if !sb.GoSet(name) || tree.LiteralChanged(db, name, sb) {
				continue
			}
			db.CopyProp(name, sb)
		}

		// TextInput contents and caret are user state, not Go state: keep them
		// whenever the file did not change the literal.
		if ti, ok := n.(*tree.TextInput); ok {
			oldTI := src.(*tree.TextInput)
			if !tree.LiteralChanged(ti.Base(), "text", oldTI.Base()) {
				ti.Base().CopyProp("text", oldTI.Base())
			}
			ti.SetCaret(oldTI.Caret())
		}
	}
}

// propNames lists the .ui property names a node exposes.
func propNames(b *tree.NodeBase) []string { return b.PropertyNames() }

// enableWatcher starts the file watcher after a successful load.
func (a *App) enableWatcher() error {
	r, err := hotreload.Watch(a.path, func(src []byte, errText string) {
		if errText != "" {
			a.Post(func() { a.reportError(fmt.Errorf("goui: %s", errText)) })
			return
		}
		a.reloadSrc(src)
	})
	if err != nil {
		return err
	}
	a.watcher = r
	return nil
}
