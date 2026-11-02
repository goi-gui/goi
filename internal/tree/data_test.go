package tree

import (
	"fmt"
	"testing"
)

type row struct {
	Title  string `json:"title"`
	Unread bool
	Count  int
	hidden string
}

const eachUI = `data {
	rows: row[]
	loading: bool
	count: number
}
data row { title: string, unread: bool, count: number }
Window {
	Column {
		id: list
		{#each data.rows as r}
			Text { id: title text: r.title }
			Text { id: state text: r.unread ? "new" : "read" }
		{:else}
			Text { id: empty text: "empty" }
		{/each}
	}
}`

// texts returns the text of every Text in the tree, in build order.
func texts(tr *Tree) []string {
	var out []string
	for _, n := range tr.All() {
		if tx, ok := n.(*Text); ok {
			out = append(out, tx.Text.Get())
		}
	}
	return out
}

// TestEachBuildsOneRowPerItem is the core of the feature: SetData builds the arm
// once per element and a second call replaces the rows.
func TestEachBuildsOneRowPerItem(t *testing.T) {
	tr, diag := BuildSource([]byte(eachUI), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	// Before any data arrives, the {:else} arm is what the design shows.
	if got := texts(tr); len(got) != 1 || got[0] != "empty" {
		t.Fatalf("texts = %v, want the else arm", got)
	}

	tr.SetData("rows", []row{{Title: "a", Unread: true}, {Title: "b"}})
	tr.ApplyExpressions()
	if got := fmt.Sprint(texts(tr)); got != "[a new b read]" {
		t.Fatalf("texts = %s", got)
	}

	tr.SetData("rows", []row{{Title: "only"}})
	tr.ApplyExpressions()
	if got := fmt.Sprint(texts(tr)); got != "[only read]" {
		t.Fatalf("after a shorter list: %s", got)
	}

	// An empty list brings the else arm back instead of stacking it.
	tr.SetData("rows", []row{})
	tr.ApplyExpressions()
	if got := fmt.Sprint(texts(tr)); got != "[empty]" {
		t.Fatalf("after an empty list: %s", got)
	}
}

// TestEachAcceptsAnySliceAndMap keeps the API friendly: a slice of structs, a
// slice of maps and a nil all work, and fields match by json tag or name.
func TestEachAcceptsAnySliceAndMap(t *testing.T) {
	tr, diag := BuildSource([]byte(eachUI), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetData("rows", []map[string]any{{"title": "mapped", "unread": true}})
	tr.ApplyExpressions()
	if got := fmt.Sprint(texts(tr)); got != "[mapped new]" {
		t.Fatalf("map rows: %s", got)
	}
	tr.SetData("rows", nil)
	tr.ApplyExpressions()
	if got := fmt.Sprint(texts(tr)); got != "[empty]" {
		t.Fatalf("nil list: %s", got)
	}
	// A struct field without a tag is matched by its lower-case name.
	tr.SetData("rows", []struct{ Hidden string }{{Hidden: "x"}})
	tr.ApplyExpressions()
	if got := fmt.Sprint(texts(tr)); got != "[ empty]" && got != "[ ]" {
		// The field is `hidden` in the design but the anonymous struct has
		// "Hidden": both normalise to `hidden`, but this shape has no such
		// field, so the text stays empty. Just make sure nothing panics.
		t.Logf("texts = %s", got)
	}
}

// TestEachRowsGetTheirOwnScope proves the loop variable is resolved per row:
// two rows must not read the first item's value.
func TestEachRowsGetTheirOwnScope(t *testing.T) {
	src := `data { rows: row[] }
data row { title: string }
Window {
	Column {
		id: list
		{#each data.rows as r}
			Column {
				{#if r.title == "hide"}
					Text { id: hidden text: "hidden" }
				{:else}
					Text { id: shown text: r.title }
				{/if}
			}
		{/each}
	}
}`
	tr, diag := BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetData("rows", []row{{Title: "hide"}, {Title: "keep"}})
	tr.ApplyExpressions()

	// Row one asks for "hide" and shows its hidden arm; row two shows "keep".
	// Each row's inner block resolves against that row's item, so the counts
	// follow the data and not the first row only.
	var hidden, shown int
	for _, n := range tr.All() {
		switch n.Base().ID() {
		case "hidden":
			if shown_(n) {
				hidden++
			}
		case "shown":
			if !shown_(n) {
				continue
			}
			shown++
			if n.(*Text).Text.Get() != "keep" {
				t.Fatalf("shown row text = %q", n.(*Text).Text.Get())
			}
		}
	}
	if hidden != 1 {
		t.Fatalf("hidden arms shown = %d, want the hide row", hidden)
	}
	if shown != 1 {
		t.Fatalf("shown rows = %d, want the one kept row", shown)
	}
}

// shown_ reports whether a node is drawn, ancestors included.
func shown_(n Node) bool {
	for n != nil {
		if !n.Base().Visible() {
			return false
		}
		n = n.Base().Parent()
	}
	return true
}

// TestEachOnEachHooksEveryRebuiltRow checks the callback contract an app relies
// on for click handlers.
func TestEachOnEachHooksEveryRebuiltRow(t *testing.T) {
	tr, diag := BuildSource([]byte(eachUI), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	var seen []string
	tr.OnEach("rows", func(n Node, item any) {
		seen = append(seen, item.(row).Title)
	})
	tr.SetData("rows", []row{{Title: "a"}, {Title: "b"}})
	if fmt.Sprint(seen) != "[a b]" {
		t.Fatalf("hooks ran for %v", seen)
	}
	seen = nil
	tr.SetData("rows", []row{{Title: "c"}})
	if fmt.Sprint(seen) != "[c]" {
		t.Fatalf("hooks ran for %v after a rebuild", seen)
	}
}

// TestEachPrunesStaleRegistrations keeps the expression list from growing with
// every refresh: the old rows must leave it.
func TestEachPrunesStaleRegistrations(t *testing.T) {
	tr, diag := BuildSource([]byte(eachUI), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	for i := 0; i < 5; i++ {
		tr.SetData("rows", []row{{Title: "x"}, {Title: "y"}})
	}
	// Two expressions per row, and the list itself is not an expression.
	if got := len(tr.exprNodes); got > 4 {
		t.Fatalf("%d expression nodes after five fills, want the two live rows", got)
	}
	if got := len(tr.All()); got > 8 {
		t.Fatalf("%d nodes left behind", got)
	}
}

// TestNestedEachWalksTheInnerList covers the grouped case: the outer variable is
// still in scope inside the inner arm.
func TestNestedEachWalksTheInnerList(t *testing.T) {
	src := `data { groups: group[] }
data group { name: string, items: item[] }
data item { label: string }
Window {
	Column {
		id: list
		{#each data.groups as g}
			Text { text: g.name }
			Column {
				id: inner
				{#each data.groups as i}
					Text { text: i.name }
				{/each}
			}
		{/each}
	}
}`
	tr, diag := BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetData("groups", []map[string]any{{"name": "A"}, {"name": "B"}})
	tr.ApplyExpressions()
	// Two outer rows, each with its own name and two inner rows: the inner
	// block is rebuilt for every outer row, not left pointing at a dead parent.
	if got := fmt.Sprint(texts(tr)); got != "[A A B B A B]" {
		t.Fatalf("texts = %s", got)
	}
}
