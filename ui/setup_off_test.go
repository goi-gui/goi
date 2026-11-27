//go:build !setup && !all

package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/syntax"
)

// TestSetupNeedsTheTag keeps a core build honest: a design with setup fails
// instead of silently reading its names as literals.
func TestSetupNeedsTheTag(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte("setup { var x = 1 }\nWindow {}"), "Main.goi")
	if !diag.Empty() {
		t.Fatal(diag)
	}
	if _, err := loadSetups(f); err == nil {
		t.Fatal("a core build accepted a setup block")
	}
	b, err := loadSetups(&syntax.File{})
	if err != nil {
		t.Fatal(err)
	}
	if b.hook != nil || b.bind != nil {
		t.Fatal("a core build returned setup hooks")
	}
}
