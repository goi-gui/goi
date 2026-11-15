package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// absRect is the node's box in window coordinates, the same walk paint uses.
func absRect(n tree.Node) tree.Rect {
	b := n.Base().Bounds()
	x, y := b.X, b.Y
	for p := n.Base().Parent(); p != nil; p = p.Base().Parent() {
		pb := p.Base().Bounds()
		x += pb.X
		y += pb.Y
		sx, sy := p.Base().ScrollShift()
		x += sx
		y += sy
	}
	return tree.Rect{X: x, Y: y, W: b.W, H: b.H}
}

// TestComponentBoxPropsApply is the audit guard: the common box properties used
// to be ignored by components, which paint their own M3 surface. Every one in
// this list must show the design's background and border, including controls
// that paint only a small part of their box.
func TestComponentBoxPropsApply(t *testing.T) {
	src := `Window {
		Column {
			padding: 20
			spacing: 12
			Button { id: b label: "B" background: #112233 borderColor: #445566 borderWidth: 2 radius: 6 }
			Card { id: c label: "C" width: 180 height: 72 background: #112233 borderColor: #445566 borderWidth: 2 radius: 6 }
			TextField { id: f width: 180 background: #112233 borderColor: #445566 borderWidth: 2 radius: 6 }
			Switch { id: s label: "S" background: #112233 borderColor: #445566 borderWidth: 2 radius: 6 }
			Checkbox { id: k label: "K" background: #112233 borderColor: #445566 borderWidth: 2 radius: 6 }
			IconButton { id: i icon: add background: #112233 borderColor: #445566 borderWidth: 2 radius: 6 }
			Divider { id: d width: 180 background: #112233 borderColor: #445566 borderWidth: 2 radius: 6 }
			Slider { id: r width: 180 background: #112233 borderColor: #445566 borderWidth: 2 radius: 6 }
			Tabs { id: t background: #112233 borderColor: #445566 borderWidth: 2 radius: 6
				Tab { label: "One" selected: true }
				Tab { label: "Two" }
			}
		}
	}`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 800)
	l := &List{}
	l.Build(tr, 400, 800)

	for _, id := range []string{"b", "c", "f", "s", "k", "i", "d", "r", "t"} {
		n, ok := tr.ByID(id)
		if !ok {
			t.Fatalf("no %s", id)
		}
		r := absRect(n)
		var bg, border bool
		for _, op := range l.Ops {
			if op.Kind != Fill || !rectEqual(op.Rect, r) {
				continue
			}
			if op.Color == 0x112233FF && op.Radius[0] > 0 {
				bg = true
			}
			if op.BorderColor == 0x445566FF && op.BorderWidth == 2 {
				border = true
			}
		}
		if !bg {
			t.Errorf("%s: no full-box background op", id)
		}
		if !border {
			t.Errorf("%s: no full-box border op", id)
		}
	}
}

// TestComponentShadowAndPadding covers the two box properties that do not go
// through the surface: `shadow` replaces a component's own elevation, and
// `padding` grows an auto-sized box the way it grows a padded Text.
func TestComponentShadowAndPadding(t *testing.T) {
	src := `Window {
		Column {
			Button { id: plain label: "Shadow" }
			Button { id: shadow label: "Shadow" shadow: 2 3 9 #00000080 }
			Button { id: padded label: "Shadow" padding: 12 }
		}
	}`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	plain, _ := tr.ByID("plain")
	padded, _ := tr.ByID("padded")
	pb, db := plain.Base().Bounds(), padded.Base().Bounds()
	if db.H != pb.H+24 || db.W != pb.W+24 {
		t.Errorf("padding did not grow the box: %v -> %v", pb, db)
	}

	l := list(t, `Window { Column { Button { label: "Shadow" shadow: 2 3 9 #00000080 } } }`, 400, 400)
	shadows := 0
	for _, op := range l.Ops {
		if op.Kind == Shadow {
			shadows++
			if op.Color != 0x00000080 {
				t.Errorf("shadow colour %08x, want the design's", op.Color)
			}
		}
	}
	// A filled button carries one elevation layer of its own; the design's
	// shadow replaces it rather than stacking on top.
	if shadows != 1 {
		t.Errorf("%d shadow ops, want 1", shadows)
	}
}

// TestComponentColorTintsTheForeground covers `color`, the one visual property
// outside the common box: a Button recolours its label and a TextField its
// value.
func TestComponentColorTintsTheForeground(t *testing.T) {
	for _, tc := range []struct {
		src, run string
		want     uint32
	}{
		{`Window { Column { Button { label: "B" color: #ff0000ff } } }`, "B", 0xff0000ff},
		{`Window { Column { TextField { label: "F" text: "v" color: #00ff00ff } } }`, "v", 0x00ff00ff},
	} {
		got := uint32(0)
		for _, op := range list(t, tc.src, 400, 300).Ops {
			if op.Kind == Text && op.Text == tc.run {
				got = op.Color
			}
		}
		if got != tc.want {
			t.Errorf("%q: text colour %08x, want %08x", tc.run, got, tc.want)
		}
	}
}
