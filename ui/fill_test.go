package ui

import "testing"

const fillUI = `Window {
	Column {
		id: list
		ListItem { template: true label: "Item" }
	}
}`

type fillItem struct{ label string }

// Fill builds one row per item from the marked template, and a second call
// replaces the rows instead of stacking them.
func TestFillRebuildsFromTheTemplate(t *testing.T) {
	_, w := reloadApp(t, fillUI)
	list := MustByID[*Column](w, "list")
	fill := func(row *ListItem, it fillItem) { row.Label.Set(it.label) }

	if err := Fill(w, list, []fillItem{{"a"}, {"b"}, {"c"}}, fill); err != nil {
		t.Fatal(err)
	}
	kids := list.Base().Children()
	if len(kids) != 4 {
		t.Fatalf("%d children, want the prototype plus three rows", len(kids))
	}
	if !kids[0].Base().Template() || kids[0].Base().Visible() {
		t.Fatal("the prototype did not stay hidden")
	}
	for i, name := range []string{"a", "b", "c"} {
		row := kids[i+1].(*ListItem)
		if row.Label.Get() != name {
			t.Fatalf("row %d label = %q, want %q", i, row.Label.Get(), name)
		}
		if row.Base().Template() {
			t.Fatal("a copy is still marked as a template")
		}
	}

	if err := Fill(w, list, []fillItem{{"only"}}, fill); err != nil {
		t.Fatal(err)
	}
	kids = list.Base().Children()
	if len(kids) != 2 {
		t.Fatalf("%d children after a refresh, want the prototype plus one row", len(kids))
	}
	if got := kids[1].(*ListItem).Label.Get(); got != "only" {
		t.Fatalf("label = %q, want only", got)
	}
}

func TestFillNeedsATemplateChild(t *testing.T) {
	_, w := reloadApp(t, `Window {
	Column {
		id: list
		Text { text: "x" }
	}
}`)
	list := MustByID[*Column](w, "list")
	if err := Fill(w, list, []int{1}, func(*ListItem, int) {}); err == nil {
		t.Fatal("Fill accepted a container with no template child")
	}
}
