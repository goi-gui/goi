package layout

import (
	"testing"

	"github.com/goi-gui/goi/internal/tree"
)

// TestMermaidKeepsItsAspect: the diagram's own pixels are the natural size, a
// declared width scales the height with it, and a column narrower than the
// diagram shrinks both instead of overflowing.
func TestMermaidKeepsItsAspect(t *testing.T) {
	tr := build(t, `Window { Column {
		Mermaid { id: natural text: "flowchart LR\n A-->B" }
		Mermaid { id: scaled width: 300 text: "flowchart LR\n A-->B" }
		Mermaid { id: fitted text: "flowchart LR\n A-->B" }
	} }`)
	var nodes []*tree.Mermaid
	for _, n := range tr.All() {
		if m, ok := n.(*tree.Mermaid); ok {
			m.IntrinsicW, m.IntrinsicH = 400, 200
			nodes = append(nodes, m)
		}
	}
	if len(nodes) != 3 {
		t.Fatalf("found %d Mermaid nodes", len(nodes))
	}
	New(nil).Layout(tr.Root, 320, 900)
	if b := nodes[0].Base().Bounds(); b.W != 320 || b.H != 160 {
		t.Fatalf("fitted width = %gx%g, want 320x160", b.W, b.H)
	}
	if b := nodes[1].Base().Bounds(); b.W != 300 || b.H != 150 {
		t.Fatalf("scaled = %gx%g, want 300x150", b.W, b.H)
	}
	if b := nodes[2].Base().Bounds(); b.W != 320 || b.H != 160 {
		t.Fatalf("fitted = %gx%g, want 320x160", b.W, b.H)
	}
}
