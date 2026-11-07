package schema

import (
	"testing"

	"github.com/goi-gui/goi/internal/syntax"
)

// TestCursorPropAcceptsEveryCSSValue keeps the property list honest: every CSS
// cursor name validates, and an unknown one is a normal "not a value" error.
func TestCursorPropAcceptsEveryCSSValue(t *testing.T) {
	for _, name := range CursorValues {
		src := "Window { Column { cursor: " + name + " } }"
		f, diag := syntax.Parse([]byte(src))
		if !diag.Empty() {
			t.Fatalf("%s: parse: %v", name, diag)
		}
		if _, d := Validate(f); !d.Empty() {
			t.Fatalf("%s: %v", name, d)
		}
	}
	f, _ := syntax.Parse([]byte("Window { Column { cursor: hand } }"))
	if _, d := Validate(f); d.Empty() {
		t.Fatal("an unknown cursor name was accepted")
	}
}
