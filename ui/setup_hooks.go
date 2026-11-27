package ui

import "github.com/goi-gui/goi/internal/tree"

// setupBuild is what loadSetups gives the loader in either build. hook resolves
// one file's setup, bind attaches the live tree after it exists, and err reports
// a file whose setup failed to compile while the tree was built.
type setupBuild struct {
	hook     func(string, string) tree.SetupScope
	bind     func(*tree.Tree)
	err      func() error
	instance func(string, string)
	visible  func(string, string, bool)
	start    func(func(func()), func(error))
	stop     func()
}

// noteSetups creates the setup instance of every scope the tree holds, so a
// file whose setup only has onMount still mounts. Scopes are one per fragment
// call site; rows cloned from the same call share it.
func noteSetups(b setupBuild, tr *tree.Tree) {
	if b.instance == nil || tr == nil {
		return
	}
	seen := map[string]bool{}
	for _, n := range tr.All() {
		base := n.Base()
		if base == nil || base.AST == nil || base.AST.Scope == "" || !visibleIn(n) {
			continue
		}
		key := base.AST.Src + "\x00" + base.AST.Scope
		if seen[key] {
			continue
		}
		seen[key] = true
		b.instance(base.AST.Src, base.AST.Scope)
	}
	if root := tr.Root; root != nil {
		if base := root.Base(); base != nil && base.AST != nil {
			b.instance(base.AST.Src, "")
		}
	}
}

// visibleIn reports whether n and every ancestor are shown, so a hidden
// conditional arm does not mount its setup.
func visibleIn(n tree.Node) bool {
	for ; n != nil; n = n.Base().Parent() {
		base := n.Base()
		if base == nil || !base.Visible() {
			return false
		}
	}
	return true
}

// startSetups queues the mount hooks after the first painted frame: the first
// post runs at the start of that frame and queues the second, which the next
// frame drains. Loading and the first paint therefore never wait for a hook.
func (a *App) startSetups(b setupBuild) {
	if b.start == nil {
		return
	}
	a.setupGen++
	gen := a.setupGen
	a.setupStop = b.stop
	a.markDirty(tree.DirtyLayout)
	a.Post(func() {
		a.Post(func() {
			if a.setupGen != gen {
				return
			}
			b.start(a.setupPost, a.reportError)
		})
	})
}

// setupPost queues a later mount, then asks for a frame so expressions see the
// state the hook changed.
func (a *App) setupPost(fn func()) {
	if fn == nil {
		return
	}
	a.Post(func() {
		fn()
		a.markDirty(tree.DirtyLayout)
	})
}

// stopSetups runs onUnmount for the current design and cancels a pending start.
func (a *App) stopSetups() {
	a.setupGen++
	if a.setupStop != nil {
		a.setupStop()
		a.setupStop = nil
	}
}
