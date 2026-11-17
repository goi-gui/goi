package paint

import (
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// textFieldPaint builds one field, applies state, and paints it.
func textFieldPaint(t *testing.T, src string, state func(*tree.TextField)) (*List, *tree.TextField, tree.Rect) {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 300)
	n, _ := tr.ByID("f")
	tf := n.(*tree.TextField)
	if state != nil {
		state(tf)
	}
	l := &List{}
	l.Build(tr, 400, 300)
	return l, tf, absolute(tf)
}

// labelOp finds the field's label text op.
func labelOp(l *List, label string) *Op {
	for i := range l.Ops {
		if l.Ops[i].Kind == Text && l.Ops[i].Text == label && !l.Ops[i].Icon {
			return &l.Ops[i]
		}
	}
	return nil
}

// TestTextFieldLabelPositions pins the float rule: content, a placeholder or
// focus lifts the label; an empty field with neither keeps it inside. Outlined
// puts the floated label on the border, filled keeps it in the top band.
func TestTextFieldLabelPositions(t *testing.T) {
	for _, variant := range []string{"filled", "outlined"} {
		for _, tc := range []struct {
			name, extra string
			state       func(*tree.TextField)
			floated     bool
		}{
			{"empty", "", nil, false},
			{"text", `text: "Ada"`, nil, true},
			{"placeholder", `placeholder: "you@example.com"`, nil, true},
			{"focused", "", func(tf *tree.TextField) { tf.Focused = true }, true},
		} {
			t.Run(variant+"/"+tc.name, func(t *testing.T) {
				src := `Window { Column { TextField { id: f variant: ` + variant + ` label: "Name" ` + tc.extra + ` width: 260 } } }`
				l, _, box := textFieldPaint(t, src, tc.state)
				op := labelOp(l, "Name")
				if op == nil {
					t.Fatal("no label")
				}
				size := float64(op.FontSize)
				want := box.Y + box.H/2 + size*0.342
				if tc.floated {
					top := box.Y
					if variant == "filled" {
						top += 16
					}
					want = top + size*0.342
					if size > 13 {
						t.Fatalf("floated label size = %v, want the small style", size)
					}
				} else if size < 15 {
					t.Fatalf("resting label size = %v, want the value style", size)
				}
				if math.Abs(float64(op.TextY)-want) > 1 {
					t.Fatalf("label baseline = %v, want %v (floated %v)", op.TextY, want, tc.floated)
				}
			})
		}
	}
}

// TestOutlinedLabelCutsTheBorder: the top line stops at each side of a floated
// label instead of running through it.
func TestOutlinedLabelCutsTheBorder(t *testing.T) {
	l, _, box := textFieldPaint(t, `Window {
		Column { TextField { id: f variant: outlined label: "Name" text: "Ada" width: 260 } }
	}`, nil)
	op := labelOp(l, "Name")
	if op == nil {
		t.Fatal("no label")
	}
	want := m3.Light().Colors.Outline
	var top, left, right bool
	for _, o := range l.Ops {
		if o.Kind != Fill || o.BorderColor != want || o.Clip[3] <= 0 {
			continue
		}
		if math.Abs(float64(o.Clip[1])-box.Y) > 0.5 {
			continue
		}
		top = true
		if float64(o.Clip[0]+o.Clip[2]) <= float64(op.TextX)+labelInk(t, op)/2 {
			left = true
		}
		if float64(o.Clip[0]) >= float64(op.TextX)+labelInk(t, op)/2 {
			right = true
		}
	}
	if !top || !left || !right {
		t.Fatalf("outline top=%v left=%v right=%v:\n%s", top, left, right, l.Dump())
	}
}

// labelInk approximates the glyph run the label op occupies.
func labelInk(t *testing.T, op *Op) float64 {
	t.Helper()
	return float64(len([]rune(op.Text))) * float64(op.FontSize) * 0.55
}

// TestTextFieldLabelInterpolates checks the animation frame paint: a half-way
// label sits between the resting line and its floated place, at a size in
// between the two styles.
func TestTextFieldLabelInterpolates(t *testing.T) {
	l, tf, box := textFieldPaint(t, `Window {
		Column { TextField { id: f variant: outlined label: "Name" text: "Ada" width: 260 } }
	}`, func(tf *tree.TextField) {
		tf.LabelPosed = true
		tf.LabelFloat = 0.5
	})
	op := labelOp(l, "Name")
	if op == nil {
		t.Fatal("no label")
	}
	size := float64(op.FontSize)
	if size <= 12.1 || size >= 15.9 {
		t.Fatalf("mid label size = %v, want between 12 and 16", size)
	}
	if float64(op.TextY) <= box.Y+size*0.342+0.5 || float64(op.TextY) >= box.Y+box.H/2+size*0.342-0.5 {
		t.Fatalf("mid label baseline = %v, want between the border and the middle", op.TextY)
	}
	_ = tf
}

// TestPlaceholderShowsUnderAFloatedLabel: a placeholder alone floats the label
// but stays in the value area, not behind it.
func TestPlaceholderShowsUnderAFloatedLabel(t *testing.T) {
	l, _, box := textFieldPaint(t, `Window {
		Column { TextField { id: f variant: outlined label: "Name" placeholder: "you@example.com" width: 260 } }
	}`, nil)
	label := labelOp(l, "Name")
	hint := labelOp(l, "you@example.com")
	if label == nil || hint == nil {
		t.Fatalf("label=%v placeholder=%v", label, hint)
	}
	if hint.TextY <= label.TextY {
		t.Fatalf("placeholder baseline = %v, label = %v: it should sit below", hint.TextY, label.TextY)
	}
	if float64(hint.TextY) < box.Y+box.H/2 {
		t.Fatalf("placeholder baseline = %v, want it in the value area", hint.TextY)
	}
}

// TestTextFieldValueIsVerticallyPlaced: an outlined label eats no room inside
// the box, so its value and placeholder sit on the field's centre line; a
// filled field keeps them under the label band.
func TestTextFieldValueIsVerticallyPlaced(t *testing.T) {
	for _, variant := range []string{"filled", "outlined"} {
		for _, tc := range []struct{ name, body, text string }{
			{"value", `text: "Ada"`, "Ada"},
			{"placeholder", `placeholder: "you@example.com"`, "you@example.com"},
		} {
			t.Run(variant+"/"+tc.name, func(t *testing.T) {
				src := `Window { Column { TextField { id: f variant: ` + variant + ` label: "Name" ` + tc.body + ` width: 260 } } }`
				l, _, box := textFieldPaint(t, src, nil)
				op := labelOp(l, tc.text)
				if op == nil {
					t.Fatalf("no %q", tc.text)
				}
				centre := float64(op.TextY) - float64(op.FontSize)*0.342
				want := box.Y + box.H/2
				if variant == "filled" {
					want = box.Y + 35 // the 22..48 value band
				}
				if math.Abs(centre-want) > 1 {
					t.Fatalf("%s centre = %v, want %v", tc.text, centre, want)
				}
			})
		}
	}
}
