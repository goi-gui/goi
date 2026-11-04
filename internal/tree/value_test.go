package tree

import (
	"strings"
	"testing"
)

// src replaces § with a backtick, so a test can write a template string inside
// an ordinary raw literal.
func tpl(s string) []byte { return []byte(strings.ReplaceAll(s, "§", "`")) }

// TestBridgeDefaultBeforeGoSets pins the two fallbacks of a bridge variable: the
// declared default, and the type's zero value when it declares none.
func TestBridgeDefaultBeforeGoSets(t *testing.T) {
	tr, diag := BuildSource(tpl(`bridge {
	count: number = 3
	name: string = "guest"
	other: number
	flag: bool
}
Window {
	Column {
		Text { id: a text: §${bridge.count}§ }
		Text { id: b text: §${bridge.name}§ }
		Text { id: c text: §${bridge.other}§ }
		Rectangle { id: d visible: bridge.flag }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()

	want := map[string]string{"a": "3", "b": "guest", "c": "0"}
	for id, w := range want {
		if got, _ := tr.ids[id].Base().PropValue("text"); got != w {
			t.Fatalf("%s text = %v, want %q", id, got, w)
		}
	}
	if tr.ids["d"].Base().Visible() {
		t.Fatal("an unset bool is not the zero value")
	}

	// Go still wins once it sets one.
	tr.SetData("count", 7)
	tr.ApplyExpressions()
	if got, _ := tr.ids["a"].Base().PropValue("text"); got != "7" {
		t.Fatalf("after SetData text = %v, want 7", got)
	}
}

// TestTemplateInterpolatesValues covers the backtick form: literals, several
// holes, and the formatting of each value type.
func TestTemplateInterpolatesValues(t *testing.T) {
	tr, diag := BuildSource(tpl(`bridge {
	count: number = 41
	name: string = "ada"
}
Window {
	Column {
		Text { id: t text: §${bridge.name} has ${bridge.count} items§ }
		Text { id: only text: §plain§ }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()
	if got, _ := tr.ids["t"].Base().PropValue("text"); got != "ada has 41 items" {
		t.Fatalf("text = %v", got)
	}
	if got, _ := tr.ids["only"].Base().PropValue("text"); got != "plain" {
		t.Fatalf("text = %v", got)
	}
}

// TestArithmeticAndConcat is the `+` rule: two numbers add, anything else is
// joined as text.
func TestArithmeticAndConcat(t *testing.T) {
	tr, diag := BuildSource(tpl(`bridge {
	a: number = 6
	b: number = 3
}
Window {
	Column {
		Text { id: sum text: §${bridge.a + bridge.b}§ }
		Text { id: join text: "n=" + bridge.a }
		Text { id: mix text: bridge.b * 2 + "px" }
		Rectangle { id: box width: bridge.a - bridge.b }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()

	for id, want := range map[string]string{"sum": "9", "join": "n=6", "mix": "6px"} {
		if got, _ := tr.ids[id].Base().PropValue("text"); got != want {
			t.Fatalf("%s text = %v, want %q", id, got, want)
		}
	}
	if got, _ := tr.ids["box"].Base().PropValue("width"); got != float64(3) {
		t.Fatalf("width = %v, want 3", got)
	}
}
