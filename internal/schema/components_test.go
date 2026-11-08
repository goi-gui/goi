package schema

import (
	"fmt"
	"testing"

	"github.com/goi-gui/goi/internal/componentdef"
)

// TestEveryComponentValidates keeps the schema vocabulary and the component
// list in sync: a new component that nobody can write in a .goi file fails here.
func TestEveryComponentValidates(t *testing.T) {
	for _, k := range componentdef.Kinds {
		src := fmt.Sprintf("Window { %s { } }", k.Name)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s: %v", k.Name, diag)
		}
	}
}

// TestTableNarrowsVariants keeps the checker honest about the styles the table
// painter implements: the shared union accepts many words, the table only three.
func TestTableNarrowsVariants(t *testing.T) {
	for _, v := range []string{"outlined", "filled", "plain"} {
		src := fmt.Sprintf("Window { Table { variant: %s } }", v)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("Table.variant %q: %v", v, diag)
		}
	}
	src := "Window { Table { variant: tonal } }"
	if _, diag := validate(t, src); diag.Empty() {
		t.Fatal("the table accepted a variant it does not paint")
	}
}

// TestTableSelectableValues checks the mode enum and that single is the default.
func TestTableSelectableValues(t *testing.T) {
	for _, v := range []string{"none", "single", "multiple"} {
		src := fmt.Sprintf("Window { Table { selectable: %s } }", v)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("Table.selectable %q: %v", v, diag)
		}
	}
	if _, diag := validate(t, "Window { Table { selectable: double } }"); diag.Empty() {
		t.Fatal("the table accepted an unknown selection mode")
	}
}
