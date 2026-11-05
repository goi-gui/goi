package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestChipLabelStaysInside covers every chip shape: plain, leading icon, the
// check a selected chip shows in its place, and a trailing icon. Each end costs
// 16dp, or 8+18+8 with an icon, and the label box must have room for the whole
// label inside that.
func TestChipLabelStaysInside(t *testing.T) {
	for _, src := range []string{
		`Chip { id: c label: "Input" variant: input }`,
		`Chip { id: c label: "Input" variant: input selected: true }`,
		`Chip { id: c label: "Input" icon: event }`,
		`Chip { id: c label: "Input" trailingIcon: close }`,
		`Chip { id: c label: "Input" icon: event trailingIcon: close }`,
		`Chip { id: c label: "Input" icon: event selected: true }`,
	} {
		tr, diag := tree.BuildSource([]byte("Window { Column { "+src+" } }"), nil)
		if !diag.Empty() {
			t.Fatalf("%s: %v", src, diag)
		}
		layout.New(nil).Layout(tr.Root, 400, 300)
		l := &List{}
		l.Build(tr, 400, 300)

		node, ok := tr.ByID("c")
		if !ok {
			t.Fatal("chip missing")
		}
		chip := absolute(node)

		var label *Op
		for i := range l.Ops {
			if l.Ops[i].Kind == Text && l.Ops[i].Text == "Input" {
				label = &l.Ops[i]
			}
		}
		if label == nil {
			t.Fatalf("%s: no label op", src)
		}
		// The label starts at the leading inset and must end before the
		// trailing one, inside the pill.
		if start := float64(label.Rect[0]); start < chip.X+15.5 {
			t.Fatalf("%s: label starts at %v, inside the %v padding", src, start, chip.X)
		}
		if want := chip.Right() - float64(label.Rect[0]) - float64(label.Rect[2]); want < 15.5 {
			t.Fatalf("%s: label ends %v from the right edge", src, want)
		}
		// And the room it gets must fit the label: a short box is what squeezed
		// the text past the pill before.
		need := float64(len("Input")) * float64(label.FontSize) * 0.55
		if float64(label.BoxW)+0.5 < need {
			t.Fatalf("%s: label box %v is narrower than the label (%v)", src, label.BoxW, need)
		}
	}
}

// TestSelectingAChipRelayouts guards the other half of the bug: the check icon
// widens the chip, so selecting it has to re-run layout, not only repaint.
func TestSelectingAChipRelayouts(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column { Chip { id: c label: "Input" variant: input } } }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	node, _ := tr.ByID("c")
	c := node.(interface{ M3() *tree.Component }).M3()
	before := node.Base().Bounds().W
	c.Selected.Set(true)
	if kind, dirty := node.Base().Dirty(); !dirty || kind != tree.DirtyLayout {
		t.Fatalf("selected marked %v, want a layout change", kind)
	}
	layout.New(nil).Layout(tr.Root, 400, 300)
	if after := node.Base().Bounds().W; after <= before {
		t.Fatalf("chip width %v -> %v, the check has no room", before, after)
	}
}

// absolute walks up to a window-space rect, the same way hit testing does.
func absolute(n tree.Node) tree.Rect {
	b := n.Base().Bounds()
	for p := n.Base().Parent(); p != nil; p = p.Base().Parent() {
		pb := p.Base().Bounds()
		b.X += pb.X
		b.Y += pb.Y
	}
	return b
}
