package tree

import "testing"

// TestRemoveAndFind covers the editing pair: a node leaves the tree with its id
// index entry, and Find still reaches an id that ByID does not index.
func TestRemoveAndFind(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Text { id: row text: "item" }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	row, _ := tr.ByID("row")
	col := row.Base().Parent()

	if found, ok := Find(row, "row"); !ok || found != row {
		t.Fatalf("Find = %v, %v", found, ok)
	}
	tr.Remove(row)
	if _, ok := tr.ByID("row"); ok {
		t.Fatal("a removed id is still indexed")
	}
	if len(col.Base().Kids()) != 0 {
		t.Fatalf("children after remove = %d", len(col.Base().Kids()))
	}
}
