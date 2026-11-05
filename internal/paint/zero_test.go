package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestZeroAreaNodeDrawsNothing guards the collapsed FAB menu: its actions get no
// bounds, and a label or an icon on a zero-size node used to land on the
// parent's origin.
func TestZeroAreaNodeDrawsNothing(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		FABMenu { id: menu icon: add
			FAB { icon: edit size: sm }
		}
	} }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 300, 300)
	l := &List{}
	l.Build(tr, 300, 300)
	// Only the menu's own "add" icon should be drawn: the action's "edit" glyph
	// has nowhere to be.
	var icons []string
	for _, op := range l.Ops {
		if op.Kind == Text {
			icons = append(icons, op.Text)
		}
	}
	if len(icons) != 1 || icons[0] != "add" {
		t.Fatalf("drawn glyphs = %v, want only the FAB's", icons)
	}
}
