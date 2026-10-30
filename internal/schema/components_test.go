package schema

import (
	"fmt"
	"testing"

	"github.com/ksckaan1/goi/internal/componentdef"
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
