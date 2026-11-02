package tree

import "github.com/ksckaan1/goi/internal/syntax"

// condBranch is one arm of an {#if} block. A block adds no node of its own: the
// arm's elements are ordinary children of the enclosing element, and the arm
// only toggles their visibility. Layout already skips an invisible subtree, so a
// hidden arm costs nothing beyond its build.
//
// The block is a chain: an arm shows when it is the first arm whose condition
// holds, and the else arm (arm -1) shows when none does.
type condBranch struct {
	conds []syntax.Value
	arm   int
	// parent is the arm of the enclosing block, if any: a nested {#if} only
	// shows inside a shown outer arm.
	parent *condBranch
	// scope is the loop scope the arm was built in, so a condition can read
	// its {#each} variable.
	scope *scope
	nodes []*NodeBase
	// own is each node's own `visible` literal, so the toggling never loses a
	// row that the design hid on purpose.
	own []bool
}

// buildBlock builds every arm of blk into parent and registers it for the
// per-frame pass.
func (t *Tree) buildBlock(blk *syntax.Block, parent *NodeBase, hooks *Hooks, outer *condBranch) error {
	var conds []syntax.Value
	for _, br := range blk.Branches {
		if br.Cond != nil {
			conds = append(conds, br.Cond)
		}
	}
	arm := 0
	for _, br := range blk.Branches {
		cb := &condBranch{conds: conds, arm: -1, parent: outer, scope: t.scope}
		if br.Cond != nil {
			cb.arm = arm
			arm++
		}
		t.conds = append(t.conds, cb)
		if err := t.buildMembers(br.Members, parent, hooks, cb); err != nil {
			return err
		}
	}
	return nil
}

// applyConds resolves every block condition and shows the matching arm. It runs
// with the expressions, so `app.dark` or a Go-set property reaches the screen.
func (t *Tree) applyConds() {
	if len(t.conds) == 0 {
		return
	}
	done := make(map[*condBranch]bool, len(t.conds))
	for _, cb := range t.conds {
		ok := t.branchOK(cb, done)
		for i, n := range cb.nodes {
			n.SetVisible(cb.own[i] && ok)
		}
	}
}

// branchOK reports whether an arm is shown, combining its own chain with every
// enclosing arm. Parents are added first, so the memo never leaves a branch
// half-resolved.
func (t *Tree) branchOK(cb *condBranch, done map[*condBranch]bool) bool {
	if v, ok := done[cb]; ok {
		return v
	}
	ok := true
	if cb.parent != nil {
		ok = t.branchOK(cb.parent, done)
	}
	if ok {
		ok = t.chainOK(cb)
	}
	done[cb] = ok
	return ok
}

func (t *Tree) chainOK(cb *condBranch) bool {
	first := -1
	for i, cond := range cb.conds {
		c := evalCtx{tree: t, sc: cb.scope, seen: map[refKey]bool{}}
		v, ok := c.eval(cond, 0)
		if b, isBool := v.(bool); ok && isBool && b {
			first = i
			break
		}
	}
	return first == cb.arm
}
