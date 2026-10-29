package ui

import (
	"fmt"

	"github.com/ksckaan1/goui/internal/asset"
	"github.com/ksckaan1/goui/internal/hotreload"
	"github.com/ksckaan1/goui/internal/syntax"
	"github.com/ksckaan1/goui/internal/tree"
)

// reload reads the whole design again and swaps it in. The watcher callback runs
// on the watcher goroutine, so the read and the swap are handed to the UI
// goroutine: a design is a set of files now, and re-reading them must not race
// with a frame.
func (a *App) reload(changed string) {
	a.Post(func() {
		if err := a.reloadNow(); err != nil {
			a.reportError(err)
		}
	})
	_ = changed
}

// reloadNow re-expands the design and replaces the window's tree.
func (a *App) reloadNow() error {
	w := a.win
	if w == nil || a.assets == nil {
		return nil
	}
	b, err := loadBundle(a.assets, a.path)
	if err != nil {
		return err
	}
	if err := a.applyReload(b.File); err != nil {
		return err
	}
	// The import graph may now include directories it did not before.
	if a.watcher != nil {
		a.rewatch(b)
	}
	return nil
}

// applyReload builds a parsed file and swaps it in. It also gives tests a way to
// reload without touching the file system.
func (a *App) applyReload(f *syntax.File) error {
	tr, err := tree.Build(f, &tree.Hooks{Marked: a.markDirty})
	if err != nil {
		return fmt.Errorf("goui: reload: %w", err)
	}
	a.loadImages(tr)
	a.swapTree(tr)
	return nil
}

// swapTree installs a freshly built tree, carrying over the state the framework
// owns. It runs on the UI goroutine.
//
// Handlers are never carried over: Bind runs again so the app can attach them to
// the new nodes. App state therefore has to live outside Bind.
func (a *App) swapTree(tr *tree.Tree) {
	w := a.win
	if w == nil {
		return
	}
	old := w.tree

	// Remember focus by id so it can be re-resolved on the new tree.
	var focusID string
	if d, ok := a.driver.(*gpuDriver); ok && d.input != nil && d.input.focus != nil {
		focusID = d.input.focus.Base().ID()
	}

	carryOver(old, tr)

	w.tree = tr
	w.title = tr.Root.Title.Get()
	if d, ok := a.driver.(*gpuDriver); ok {
		d.input.tree = tr
		d.input.focus = nil
		if focusID != "" {
			if n, ok := tr.ByID(focusID); ok {
				if k := n.Kind(); k == "TextField" || k == "SearchBar" {
					d.input.focus = comp(n)
				}
			}
		}
	}

	if err := w.Bind(w.binding); err != nil {
		a.reportError(fmt.Errorf("goui: reload: bind: %w", err))
	}
	// Everything must be repainted after a swap: the display list is rebuilt from
	// scratch and the old node pointers are gone.
	for _, n := range tr.All() {
		n.Base().MarkDirty(tree.DirtyLayout)
	}
	tr.Root.Base().MarkDirty(tree.DirtyLayout)
	a.needsFrame = true
	if a.driver != nil {
		a.driver.wake()
	}
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

		// Text field contents and caret are user state, not Go state: keep them
		// whenever the file did not change the literal.
		if k := n.Kind(); k == "TextField" || k == "SearchBar" {
			ti, oldTI := comp(n), comp(src)
			if ti != nil && oldTI != nil {
				if !tree.LiteralChanged(n.Base(), "text", src.Base()) {
					ti.Text = oldTI.Text
				}
				ti.Caret = oldTI.Caret
				ti.SelAnchor = oldTI.SelAnchor
			}
		}
	}
}

// propNames lists the .ui property names a node exposes.
func propNames(b *tree.NodeBase) []string { return b.PropertyNames() }

// enableWatcher starts watching every directory the design lives in.
func (a *App) enableWatcher() error {
	root, ok := watchRoot(a.assets)
	if !ok {
		return fmt.Errorf("goui: hot reload needs a directory container: use ui.DirFS instead of os.DirFS or an embed.FS")
	}
	r, err := hotreload.Watch([]string{root}, func(path string) { a.reload(path) })
	if err != nil {
		return err
	}
	a.watcher = r
	return nil
}

// rewatch adds directories that a reload brought into the design.
func (a *App) rewatch(b *asset.Bundle) {
	dirs := sourceDirs(b, a.assets)
	if len(dirs) == 0 {
		return
	}
	old := a.watcher
	r, err := hotreload.Watch(dirs, func(path string) { a.reload(path) })
	if err != nil || r == nil {
		return
	}
	if old != nil {
		old.Close()
	}
	a.watcher = r
}
