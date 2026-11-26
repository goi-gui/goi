package tree

import (
	"fmt"
	"strings"
	"testing"
)

type row struct {
	Title  string `goi:"title"`
	Unread bool
	Count  int
}

const eachUI = `bridge {
	rows []row
	loading bool
	count float64
}
type row { title string, unread bool, count float64 }
Window {
	Column {
		id: list
		for _, r := range bridge.rows {
			Text { id: title text: r.title }
			Text { id: state text: r.unread ? "new" : "read" }
		}
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
	// Before any data arrives the loop has no iterations.
	if got := texts(tr); len(got) != 0 {
		t.Fatalf("texts = %v, want nothing before data arrives", got)
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

	// An empty list removes every row instead of stacking them.
	tr.SetData("rows", []row{})
	tr.ApplyExpressions()
	if got := fmt.Sprint(texts(tr)); got != "[]" {
		t.Fatalf("after an empty list: %s", got)
	}
}

// TestEachAcceptsAnySliceAndMap keeps the API friendly: a slice of structs, a
// slice of maps and a nil all work, and fields match by goi tag or name.
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
	if got := fmt.Sprint(texts(tr)); got != "[]" {
		t.Fatalf("nil list: %s", got)
	}
	// A struct field without a tag is matched by its lower-case name.
	tr.SetData("rows", []struct {
		Hidden string
		Title  string
	}{{Hidden: "x", Title: "t"}})
	tr.ApplyExpressions()
	if got := fmt.Sprint(texts(tr)); got != "[t ]" {
		t.Fatalf("untagged field: %s", got)
	}
}

// TestNestedEachWalksTheInnerList covers the grouped case: the outer variable is
// still in scope inside the inner arm.
func TestNestedEachWalksTheInnerList(t *testing.T) {
	src := `bridge { groups []group }
type group { name string, items []item }
type item { label string }
Window {
	Column {
		id: list
		for _, g := range bridge.groups {
			Text { text: g.name }
			Column {
				id: inner
				for _, i := range bridge.groups {
					Text { text: i.name }
				}
			}
		}
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

// TestGoiTagNamesStructFields is the contract for a Go struct reaching a design:
// `goi:"name"` sets the design name, `goi:"-"` hides the field, and an
// unexported field is invisible.
func TestGoiTagNamesStructFields(t *testing.T) {
	type user struct {
		ID      string `goi:"id"`
		Name    string `goi:"fullName"`
		Ignored string `goi:"-"`
		private bool
	}
	u := user{ID: "1", Name: "Ada", Ignored: "no", private: true}

	for field, want := range map[string]any{"id": "1", "fullName": "Ada"} {
		got, ok := dataValue(u, field)
		if !ok || got != want {
			t.Fatalf("%s = %v, %v; want %v", field, got, ok, want)
		}
	}
	for _, hidden := range []string{"ignored", "private", "Ignored", "private"} {
		if got, ok := dataValue(u, hidden); ok {
			t.Fatalf("%s = %v, want it hidden", hidden, got)
		}
	}

	// The design reads the tagged name because the declarations say so.
	tr, diag := BuildSource([]byte(`bridge { Users []User }
type User { fullName string }
Window { Column { for _, u := range bridge.Users { Text { text: u.fullName } } } }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetData("Users", []user{u})
	tr.ApplyExpressions()
	if got := fmt.Sprint(texts(tr)); got != "[Ada]" {
		t.Fatalf("texts = %s, want the goi-tagged field", got)
	}
}

// TestCarriedDataWarnsAndResetsOnTypeChange covers a hot reload whose bridge
// declaration no longer matches the value SetData stored: the value is reset to
// its zero value with a warning, while matching values are kept.
func TestCarriedDataWarnsAndResetsOnTypeChange(t *testing.T) {
	var warnings []string
	tr, diag := BuildSource([]byte(`bridge {
	count int
	name  string
	rows  []string
}
Window { Column { Text { text: formatf("%d %s", bridge.count, bridge.name) } } }`), &Hooks{
		Data: map[string]any{"count": "seven", "name": "Kaan", "rows": []int{1, 2}},
		Warn: func(msg string) { warnings = append(warnings, msg) },
	})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if got, _ := tr.Data("count"); got != float64(0) {
		t.Fatalf("count = %v, want the zero value", got)
	}
	if got, _ := tr.Data("name"); got != "Kaan" {
		t.Fatalf("name = %v, want the carried value", got)
	}
	if got, _ := tr.Data("rows"); fmt.Sprint(got) != "[]" {
		t.Fatalf("rows = %v, want the empty zero value", got)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want one for count and one for rows", warnings)
	}
	for _, msg := range warnings {
		if !strings.Contains(msg, "no longer fits") || !strings.Contains(msg, "reset to its zero value") {
			t.Fatalf("warning = %q", msg)
		}
	}
}
