package ui

import (
	"strings"
	"testing"
)

const dataUI = `data {
	count: number
	rows: row[]
}
data row { title: string, unread: bool }
Window {
	Column {
		id: list
		Text { id: count text: data.count }
		{#each data.rows as r}
			ListItem {
				id: rowTitle
				label: r.title
				selected: r.unread
			}
		{:else}
			Text { id: empty text: "nothing" }
		{/each}
	}
}`

type dataRow struct {
	Title  string `json:"title"`
	Unread bool   `json:"unread"`
}

func labels(w *Window) []string {
	list := MustByID[*Column](w, "list")
	var out []string
	for _, c := range list.Base().Children() {
		if item, ok := c.(*ListItem); ok {
			out = append(out, item.Label.Get())
		}
	}
	return out
}

// SetData builds the rows from a slice, and OnEach hands each row the item it
// was built from, which is how a click reaches the right element.
func TestSetDataFillsRowsAndHooksEach(t *testing.T) {
	_, w := reloadApp(t, dataUI)

	SetData(w, "count", 2)
	SetData(w, "rows", []dataRow{{Title: "a", Unread: true}, {Title: "b"}})
	w.tree.ApplyExpressions() // the frame loop does this before drawing

	if got := strings.Join(labels(w), ","); got != "a,b" {
		t.Fatalf("rows = %q", got)
	}
	if got := MustByID[*Text](w, "count").Text.Get(); got != "2" {
		t.Fatalf("count = %q", got)
	}

	var clicked []string
	OnEach[*ListItem, dataRow](w, "rows", func(row *ListItem, it dataRow) {
		row.OnClick(func(ClickEvent) { clicked = append(clicked, it.Title) })
	})

	// Rows rebuilt later are hooked too.
	SetData(w, "rows", []dataRow{{Title: "c"}})
	w.tree.ApplyExpressions()
	list := MustByID[*Column](w, "list")
	var row *ListItem
	for _, c := range list.Base().Children() {
		if item, ok := c.(*ListItem); ok {
			row = item
		}
	}
	if row == nil {
		t.Fatal("no row built")
	}
	row.Click.Emit(ClickEvent{})
	if strings.Join(clicked, ",") != "c" {
		t.Fatalf("clicked = %v, want the rebuilt row", clicked)
	}

	// An empty list brings the else arm back.
	SetData(w, "rows", nil)
	if got := len(labels(w)); got != 0 {
		t.Fatalf("%d rows after an empty list", got)
	}
	if !MustByID[*Text](w, "empty").Base().Visible() {
		t.Fatal("the else arm did not come back")
	}
}

// TestSetDataReachesACondition covers the other half of the API: a bool variable
// switches an {#if} without any per-element wiring.
func TestSetDataReachesACondition(t *testing.T) {
	_, w := reloadApp(t, `data { loading: bool }
Window {
	Column {
		{#if data.loading}
			Text { id: spinner text: "wait" }
		{:else}
			Text { id: ready text: "done" }
		{/if}
	}
}`)
	spinner := MustByID[*Text](w, "spinner")
	ready := MustByID[*Text](w, "ready")

	SetData(w, "loading", true)
	w.tree.ApplyExpressions()
	if !spinner.Base().Visible() || ready.Base().Visible() {
		t.Fatal("loading did not pick the spinner arm")
	}
	SetData(w, "loading", false)
	w.tree.ApplyExpressions()
	if spinner.Base().Visible() || !ready.Base().Visible() {
		t.Fatal("the else arm did not come back")
	}
}
