package tree

import (
	"fmt"
	"sort"
	"testing"
)

// TestBridgeZeroBeforeGoSets pins the fallback of a bridge variable: the type's
// zero value, so a design reads a value before Go says anything.
func TestBridgeZeroBeforeGoSets(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge {
	Count int
	Name  string
	Ok    bool
}
Window {
	Column {
		Text { id: a text: formatf("%d", bridge.Count) }
		Text { id: b text: formatf("%s!", bridge.Name) }
		Rectangle { id: d visible: bridge.Ok }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()

	want := map[string]string{"a": "0", "b": "!"}
	for id, w := range want {
		if got, _ := tr.ids[id].Base().PropValue("text"); got != w {
			t.Fatalf("%s text = %v, want %q", id, got, w)
		}
	}
	if tr.ids["d"].Base().Visible() {
		t.Fatal("an unset bool is not the zero value")
	}

	// Go still wins once it sets one.
	tr.SetData("Count", 7)
	tr.ApplyExpressions()
	if got, _ := tr.ids["a"].Base().PropValue("text"); got != "7" {
		t.Fatalf("after SetData text = %v, want 7", got)
	}
}

// TestFormatf covers the builtin: Go's fmt verbs, several arguments, and a
// color printed as #rrggbb.
func TestFormatf(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge {
	Name  string
	Count int
	Price float64
}
Window {
	Column {
		Text { id: t text: formatf("%s has %d items at %0.2f", bridge.Name, bridge.Count, bridge.Price) }
		Text { id: hex text: formatf("%v", bridge.Price) }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetViewport(800, 600)
	tr.SetData("Name", "ada")
	tr.SetData("Count", 41)
	tr.SetData("Price", 1.5)
	tr.ApplyExpressions()
	if got, _ := tr.ids["t"].Base().PropValue("text"); got != "ada has 41 items at 1.50" {
		t.Fatalf("text = %v", got)
	}
	if got, _ := tr.ids["hex"].Base().PropValue("text"); got != "1.5" {
		t.Fatalf("text = %v", got)
	}
}

// TestArithmeticAndConcat is the `+` rule: two numbers add, anything else is
// joined as text.
func TestArithmeticAndConcat(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge {
	A int
	B int
}
Window {
	Column {
		Text { id: sum text: formatf("%d", bridge.A + bridge.B) }
		Text { id: join text: "n=" + bridge.A }
		Rectangle { id: box width: bridge.A - bridge.B }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetViewport(800, 600)
	tr.SetData("A", 6)
	tr.SetData("B", 3)
	tr.ApplyExpressions()

	for id, want := range map[string]string{"sum": "9", "join": "n=6"} {
		if got, _ := tr.ids[id].Base().PropValue("text"); got != want {
			t.Fatalf("%s text = %v, want %q", id, got, want)
		}
	}
	if got, _ := tr.ids["box"].Base().PropValue("width"); got != float64(3) {
		t.Fatalf("width = %v, want 3", got)
	}
}

// TestRangeLoopForms covers every range form the parser accepts: index and
// value, value only, `_`, a map, and a count.
func TestRangeLoopForms(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge {
	Rows  []Row
	Table map[string]string
	Count int
}
type Row { Title string }
Window {
	Column {
		for i, r := range bridge.Rows {
			Text { text: formatf("%d:%s", i, r.Title) }
		}
		for _, r := range bridge.Rows {
			Text { text: r.Title }
		}
		for i := range bridge.Rows {
			Text { text: formatf("#%d", i) }
		}
		for k, v := range bridge.Table {
			Text { text: formatf("%s=%s", k, v) }
		}
		for i := range 3 {
			Text { text: formatf("n%d", i) }
		}
		for i := range len(bridge.Rows) {
			Text { text: formatf("L%d", i) }
		}
		for i := range bridge.Count {
			Text { text: formatf("C%d", i) }
		}
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetData("Rows", []map[string]any{{"title": "a"}, {"title": "b"}})
	tr.SetData("Table", map[string]string{"x": "1", "y": "2"})
	tr.SetData("Count", 2)
	tr.ApplyExpressions()

	sorted := func() []string {
		got := texts(tr)
		sort.Strings(got)
		return got
	}
	want := []string{"#0", "#1", "0:a", "1:b", "C0", "C1", "L0", "L1", "a", "b", "n0", "n1", "n2", "x=1", "y=2"}
	if got := sorted(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("texts = %v, want %v", got, want)
	}

	// Setting the map alone rebuilds only the loop that walks it.
	tr.SetData("Table", map[string]string{"z": "9"})
	tr.ApplyExpressions()
	want = []string{"#0", "#1", "0:a", "1:b", "C0", "C1", "L0", "L1", "a", "b", "n0", "n1", "n2", "z=9"}
	if got := sorted(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after a map set: %v, want %v", got, want)
	}
}

// TestGoBuiltins covers the pure Go builtins a design can call: len and cap
// count a string, a list and a map, and min and max work over numbers and
// strings.
func TestGoBuiltins(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge {
	Rows  []Row
	Table map[string]string
	Name  string
	Count int
}
type Row { title string }
Window {
	Column {
		Text { id: rows text: formatf("%d", len(bridge.Rows)) }
		Text { id: caps text: formatf("%d", cap(bridge.Rows)) }
		Text { id: table text: formatf("%d", len(bridge.Table)) }
		Text { id: name text: formatf("%d", len(bridge.Name)) }
		Text { id: hi text: formatf("%d", max(1, bridge.Count, 3)) }
		Text { id: lo text: formatf("%d", min(1, bridge.Count, 3)) }
		Text { id: word text: max("apple", "pear") }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetData("Rows", []map[string]any{{"title": "a"}, {"title": "b"}})
	tr.SetData("Table", map[string]string{"x": "1"})
	tr.SetData("Name", "ada")
	tr.SetData("Count", 9)
	tr.ApplyExpressions()

	want := map[string]string{
		"rows": "2", "caps": "2", "table": "1", "name": "3",
		"hi": "9", "lo": "1", "word": "pear",
	}
	for id, w := range want {
		got, _ := tr.ids[id].Base().PropValue("text")
		if got != w {
			t.Fatalf("%s = %v, want %q", id, got, w)
		}
	}
}
