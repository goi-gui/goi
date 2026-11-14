package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestMermaidPaintsItsBitmap: the ui layer supplies the pixels; paint draws them
// inside the node's box with contain, not a missing-image hole.
func TestMermaidPaintsItsBitmap(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Mermaid { id: diagram text: "flowchart LR\n A-->B" }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	var m *tree.Mermaid
	for _, n := range tr.All() {
		if x, ok := n.(*tree.Mermaid); ok {
			m = x
		}
	}
	m.IntrinsicW, m.IntrinsicH = 400, 200
	m.Data = &img.Data{W: 2, H: 1, Pix: make([]byte, 8)}
	layout.New(nil).Layout(tr.Root, 320, 400)
	l := &List{}
	l.Build(tr, 320, 400)
	var bitmap *Op
	for i := range l.Ops {
		if l.Ops[i].Kind == Bitmap {
			bitmap = &l.Ops[i]
		}
	}
	if bitmap == nil {
		t.Fatal("no diagram bitmap")
	}
	if bitmap.Rect[2] != 320 || bitmap.Rect[3] != 160 {
		t.Fatalf("bitmap = %gx%g, want 320x160", bitmap.Rect[2], bitmap.Rect[3])
	}
}
