package tree

import "testing"

// TestInstantiateCopiesTemplate covers the list pattern: a hidden template, N
// copies as siblings, ids kept but not registered, and removal.
func TestInstantiateCopiesTemplate(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Text { id: rowTemplate text: "item" visible: false }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	parent, _ := tr.ByID("rowTemplate")
	col := parent.Base().Parent()

	n, err := tr.Instantiate(parent, col)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Base().Visible() {
		t.Fatal("a copy is invisible; a template hides only itself")
	}
	if got, _ := n.Base().PropValue("text"); got != "item" {
		t.Fatalf("copy text = %v", got)
	}
	// The copy does not steal the template's id.
	if got, _ := tr.ByID("rowTemplate"); got != parent {
		t.Fatal("id lookup now points at the copy")
	}
	// But the id is still reachable inside the copy.
	if found, ok := Find(n, "rowTemplate"); !ok || found != n {
		t.Fatalf("Find inside copy = %v, %v", found, ok)
	}
	if len(col.Base().Kids()) != 2 {
		t.Fatalf("children = %d, want 2", len(col.Base().Kids()))
	}
	tr.Remove(n)
	if len(col.Base().Kids()) != 1 {
		t.Fatalf("children after remove = %d", len(col.Base().Kids()))
	}
}
