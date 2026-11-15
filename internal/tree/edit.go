package tree

// Remove detaches a node and its subtree from the tree. Ids that were
// registered for the node go with it, so a later lookup is a plain not-found.
func (t *Tree) Remove(n Node) {
	if n == nil {
		return
	}
	b := n.Base()
	if b.parent == nil {
		return
	}
	p := b.parent
	for i, k := range p.kids {
		if k == b {
			p.kids = append(p.kids[:i], p.kids[i+1:]...)
			break
		}
	}
	b.parent = nil
	// Ids that were registered for this node are gone with it; a later lookup
	// of a removed id is a plain not-found, which is what an app expects.
	if b.id != "" {
		if cur, ok := t.ids[b.id]; ok && cur == n {
			delete(t.ids, b.id)
		}
	}
	p.mark(DirtyLayout)
	if b.floats {
		// A removed panel must stop lifting the branch it lived in.
		t.recomputeFloats()
	}
}

// recomputeFloats rebuilds the "subtree holds a floating panel" flags. It runs
// only when a floating subtree left the tree, so a whole-list refill of ordinary
// rows never pays for it.
func (t *Tree) recomputeFloats() {
	all := t.All()
	for _, n := range all {
		n.Base().floats = false
	}
	for _, n := range all {
		if p, ok := n.(*Popover); ok && p.Panel != nil {
			for a := p.Panel.Base(); a != nil && !a.floats; a = a.parent {
				a.floats = true
			}
		}
	}
}

// Find returns the descendant of root whose .goi id is id, root included. A
// {#each} row's ids are scoped, so a row is reachable this way but not through
// Tree.ByID.
func Find(root Node, id string) (Node, bool) {
	if root == nil || id == "" {
		return nil, false
	}
	var out Node
	var walk func(*NodeBase) bool
	walk = func(b *NodeBase) bool {
		if b.id == id {
			out = b.node
			return true
		}
		for _, k := range b.kids {
			if walk(k) {
				return true
			}
		}
		return false
	}
	walk(root.Base())
	return out, out != nil
}
