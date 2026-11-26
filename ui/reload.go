package ui

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/goi-gui/goi/internal/asset"
	"github.com/goi-gui/goi/internal/hotreload"
	"github.com/goi-gui/goi/internal/i18n"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

// reload reads the whole design again and swaps it in. The watcher callback runs
// on the watcher goroutine, so the read and the swap are handed to the UI
// goroutine: a design is a set of files now, and re-reading them must not race
// with a frame.
func (a *App) reload(changed string) {
	a.Post(func() {
		// A catalog edit only re-reads the translations: rebuilding the tree
		// would drop nothing, but it costs a layout for a text change.
		if strings.HasSuffix(changed, ".json") {
			if err := a.reloadCatalog(); err != nil {
				a.reportError(err)
			}
			return
		}
		if err := a.reloadNow(); err != nil {
			a.reportError(err)
		}
	})
}

// applyLiveAssets installs a snapshot a development server pushed and reloads
// what the changed paths reach. The wrapper sits over the embedded build, so
// everything the snapshot does not mention still comes from the wasm.
//
// A catalog edit re-reads only the translations; any other path rebuilds the
// tree, which is the same swap the native watcher uses and therefore keeps the
// Go app state that lives outside Bind.
func (a *App) applyLiveAssets(files map[string][]byte) {
	if a.assets == nil {
		return
	}
	live, ok := a.assets.(*liveAssets)
	if !ok {
		live = newLiveAssets(a.assets)
		a.assets = live
	}
	changed := live.update(files)
	if len(changed) == 0 {
		return
	}
	catalogChanged, treeChanged := false, false
	for _, name := range changed {
		if path.Dir(name) == i18n.Dir && strings.HasSuffix(name, ".json") {
			catalogChanged = true
			continue
		}
		treeChanged = true
	}
	if catalogChanged {
		if err := a.reloadCatalog(); err != nil {
			a.reportError(err)
		}
	}
	if treeChanged {
		if err := a.reloadNow(); err != nil {
			a.reportError(err)
		}
	}
}

// reloadCatalog re-reads the i18n folder and swaps it into the live tree. The
// frame loop re-resolves every t(...) afterwards, so the change shows on the
// next frame without a relayout.
func (a *App) reloadCatalog() error {
	if a.assets == nil {
		return nil
	}
	catalog, err := i18n.Load(a.assets, i18n.Dir)
	if err != nil {
		return err
	}
	a.catalog = catalog
	if a.win != nil && a.win.tree != nil {
		a.win.tree.SetI18n(catalog)
	}
	a.requestFrame()
	return nil
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
	start := time.Now()
	// The old tree's native views belong to the old nodes.
	a.closeWebviews()
	tr, err := tree.Build(f, &tree.Hooks{Marked: a.markDirty, Error: a.reportError, Media: a.mediaRequest, Post: a.Post})
	if err != nil {
		logg().Error(logCtx, "ui: hot reload failed", "error", err)
		return fmt.Errorf("goi: reload: %w", err)
	}
	a.loadImages(tr)
	a.loadMedia(tr)
	a.loadSystemFonts(tr)
	if a.win != nil {
		a.win.noteMissingFonts(tr)
	}
	a.swapTree(tr)
	logg().Debug(logCtx, "ui: hot reload", "file", a.path, "took", time.Since(start).String())
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
	if old != nil {
		carryScroll(old, tr)
	}

	w.tree = tr
	tr.SetI18n(a.catalog)
	// The desktop language is read once, so the new tree needs it too: an app
	// that never called SetLanguage still translates through it.
	if old != nil {
		tr.SetSystemLanguage(old.SystemLanguage())
	}
	tr.SetLanguage(a.language)
	tr.SetFallbackLanguage(a.fallbackLanguage)
	w.title = tr.Root.Title.Get()
	if d, ok := a.driver.(*gpuDriver); ok {
		d.input.tree = tr
		// A drag, selection or gesture may span the reload; the nodes it holds
		// belong to the old tree.
		d.input.resetPointerState()
		if focusID != "" {
			if n, ok := tr.ByID(focusID); ok {
				if k := n.Kind(); k == "TextField" || k == "SearchBar" || k == "TextArea" {
					d.input.focus = comp(n)
				}
			}
		}
	}

	if err := w.Bind(w.binding); err != nil {
		a.reportError(fmt.Errorf("goi: reload: bind: %w", err))
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
		src, ok := old.ByID(id)
		if id == "" {
			// The root has no id, but it is where the theme lives: a Go-set
			// theme has to survive a reload like any other Go value.
			if n.Base().Parent() != nil {
				continue
			}
			src, ok = old.Root, true
		}
		if !ok || src.Kind() != n.Kind() {
			continue
		}
		sb, db := src.Base(), n.Base()

		// Property values Go wrote survive, unless the .goi file changed that
		// property's literal between the two versions.
		for _, name := range propNames(db) {
			if !sb.GoSet(name) || tree.LiteralChanged(db, name, sb) {
				continue
			}
			db.CopyProp(name, sb)
			// A Go-set width beats a percentage expression in the design,
			// otherwise a resized `width: 50%` column snaps back on reload.
			if name == "width" {
				db.SetWidthPixels(db.Width())
			}
			if name == "height" {
				db.SetHeightPixels(db.Height())
			}
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

// propNames lists the .goi property names a node exposes.
func propNames(b *tree.NodeBase) []string { return b.PropertyNames() }

// scrollState is where one scrolling node was left.
type scrollState struct{ x, y float64 }

// carryScroll keeps every scrolling node where the reader left it. A node with
// an id is matched by it; one without falls back to its position in the tree,
// which survives the style edit a reload usually is. A scroller the new file
// dropped, or one that no longer scrolls, takes nothing.
func carryScroll(old, fresh *tree.Tree) {
	if old == nil || old.Root == nil || fresh.Root == nil {
		return
	}
	byID := map[string]scrollState{}
	byPath := map[string]scrollState{}
	var save func(n tree.Node, path string)
	save = func(n tree.Node, path string) {
		b := n.Base()
		if b.ScrollAxis() != tree.AxisNone {
			st := scrollState{
				x: b.ScrollOffsetFor(tree.AxisX),
				y: b.ScrollOffsetFor(tree.AxisY),
			}
			if id := b.ID(); id != "" {
				byID[id] = st
			}
			byPath[path] = st
		}
		for i, k := range b.Kids() {
			save(k.Node(), path+"/"+strconv.Itoa(i))
		}
	}
	save(old.Root, "")

	var restore func(n tree.Node, path string)
	restore = func(n tree.Node, path string) {
		b := n.Base()
		if b.ScrollAxis() != tree.AxisNone {
			st, ok := scrollState{}, false
			if id := b.ID(); id != "" {
				st, ok = byID[id]
			}
			if !ok {
				st, ok = byPath[path]
			}
			if ok {
				b.SetScrollOffsetFor(tree.AxisX, st.x)
				b.SetScrollOffsetFor(tree.AxisY, st.y)
			}
		}
		for i, k := range b.Kids() {
			restore(k.Node(), path+"/"+strconv.Itoa(i))
		}
	}
	restore(fresh.Root, "")
}

// enableWatcher starts watching the assets root, its i18n folder and every
// directory the loaded design actually reads.
func (a *App) enableWatcher(b *asset.Bundle) error {
	dirs := a.watcherDirs(b)
	if len(dirs) == 0 {
		return fmt.Errorf("goi: hot reload needs a design directory, not an embedded one")
	}
	r, err := hotreload.Watch(dirs, func(path string) { a.reload(path) })
	if err != nil {
		return err
	}
	a.watcher = r
	return nil
}

// watcherDirs is one root plus the subdirectories that hold the design files.
// A design in assets/ui would otherwise sit outside the watched root.
func (a *App) watcherDirs(b *asset.Bundle) []string {
	var dirs []string
	if b != nil {
		dirs = append(dirs, sourceDirs(b, a.assets)...)
	}
	if root, ok := watchRoot(a.assets); ok {
		dirs = append(dirs, watchDirs(root)...)
	}
	seen := map[string]bool{}
	out := dirs[:0]
	for _, dir := range dirs {
		if dir != "" && !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return out
}

// rewatch adds directories that a reload brought into the design.
func (a *App) rewatch(b *asset.Bundle) {
	dirs := a.watcherDirs(b)
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
