package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// TestSelectableTextPaintsHighlight checks that a selection is drawn under the
// glyphs, not instead of them, and only when the property is on.
func TestSelectableTextPaintsHighlight(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Text { id: body text: "hello world" width: 200 height: 20 }
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 300, 100)
	n, _ := tr.ByID("body")
	body := n.(*tree.Text)
	l := &List{}
	l.Build(tr, 300, 100)
	highlight := withAlpha(m3.Light().Colors.Primary, m3.SelectionOpacity)
	count := func() int {
		k := 0
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Color == highlight {
				k++
			}
		}
		return k
	}
	if count() != 0 {
		t.Fatal("text without a selection painted a highlight")
	}
	body.Selectable.Set(true)
	body.SelA, body.SelB = 0, 5
	l.Build(tr, 300, 100)
	if count() != 1 {
		t.Fatalf("selection highlights = %d:\n%s", count(), l.Dump())
	}
	var glyphs bool
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "hello world" {
			glyphs = true
		}
	}
	if !glyphs {
		t.Fatal("the selection replaced the text")
	}
}
