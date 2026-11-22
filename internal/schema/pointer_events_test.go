package schema

import (
	"testing"

	"github.com/goi-gui/goi/internal/syntax"
)

func TestPointerEventsValues(t *testing.T) {
	for _, v := range PointerEventsValues {
		f, diag := syntax.Parse([]byte("Window { Column { pointerEvents: " + v + " } }"))
		if !diag.Empty() {
			t.Fatalf("%s: parse: %v", v, diag)
		}
		if _, d := Validate(f); !d.Empty() {
			t.Fatalf("%s: %v", v, d)
		}
	}
	f, _ := syntax.Parse([]byte("Window { Column { pointerEvents: visiblePainted } }"))
	if _, d := Validate(f); d.Empty() {
		t.Fatal("an unsupported pointer-events value was accepted")
	}
}
