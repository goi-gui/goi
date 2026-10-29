package tree

import "fmt"

// Instantiate builds a copy of template's subtree and appends it to parent. It
// is how an app grows a list without building nodes by hand: put one hidden
// template in the .ui file, then copy it per item.
//
//	row, err := w.T().Instantiate(w.T().MustByID("rowTemplate"), list)
//
// Ids inside the copy are kept on the nodes but not registered, so they cannot
// clash with the template; look them up with Tree.Find below. The copy is made
// visible, because a template is usually declared with `visible: false`.
//
// shortcut: an expression inside a template that references a sibling by id
// resolves to the template's node, not the copy, because ids are not rewritten.
// use a Go property instead when a template needs internal references.
func (t *Tree) Instantiate(template Node, parent Node) (Node, error) {
	if template == nil || template.Base().AST == nil {
		return nil, fmt.Errorf("tree: template has no source element")
	}
	if parent == nil {
		return nil, fmt.Errorf("tree: instantiate needs a parent")
	}
	t.cloning = true
	n, err := t.build(template.Base().AST, parent.Base(), template.Base().hooks)
	t.cloning = false
	if err != nil {
		return nil, err
	}
	n.Base().SetVisible(true)
	// A copy inherits the template's breakpoint maps, so register it with the
	// same lists a freshly built node would join.
	if len(n.Base().responsive) > 0 {
		t.responsive = append(t.responsive, n.Base())
	}
	if !n.Base().exprListed && len(n.Base().exprs) > 0 {
		n.Base().exprListed = true
		t.exprNodes = append(t.exprNodes, n.Base())
	}
	t.markDirtyDeep(n.Base())
	return n, nil
}

// Remove detaches a node and its subtree from the tree. It is the inverse of
// Instantiate, and also works on a node declared in the file.
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
}

// Find returns the descendant of root whose .ui id is id, root included. It
// exists because Instantiate keeps ids on copies without registering them.
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

// markDirtyDeep schedules layout for a freshly inserted subtree, since its
// nodes were never laid out.
func (t *Tree) markDirtyDeep(b *NodeBase) {
	b.mark(DirtyLayout)
	for _, k := range b.kids {
		t.markDirtyDeep(k)
	}
}
