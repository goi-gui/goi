//go:build !setup && !all

package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/syntax"
)

// TestSetupNeedsTheTag keeps a core build honest: a design with setup fails
// instead of silently reading its names as literals.
func TestSetupNeedsTheTag(t *testing.T) {
	if _, err := loadSetups([]syntax.Setup{{Src: "Main.goi", Code: "var x = 1"}}); err == nil {
		t.Fatal("a core build accepted a setup block")
	}
	hook, err := loadSetups(nil)
	if err != nil {
		t.Fatal(err)
	}
	if hook != nil {
		t.Fatal("a core build returned a setup hook")
	}
}
