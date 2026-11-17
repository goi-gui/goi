package paint

import (
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// textAreaPaint builds one multiline field, applies state, and paints it.
func textAreaPaint(t *testing.T, src string, state func(*tree.TextArea)) (*List, *tree.TextArea, tree.Rect) {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 300)
	n, _ := tr.ByID("a")
	a := n.(*tree.TextArea)
	if state != nil {
		state(a)
	}
	l := &List{}
	l.Build(tr, 400, 300)
	return l, a, absolute(a)
}

func textOps(l *List, want ...string) map[string]Op {
	found := map[string]Op{}
	for _, op := range l.Ops {
		if op.Kind != Text || op.Icon {
			continue
		}
		for _, s := range want {
			if op.Text == s {
				found[s] = op
			}
		}
	}
	return found
}

// TestTextAreaLabelAndPlaceholder pins the label rule in both variants: content,
// a placeholder or focus floats it; the placeholder keeps its line below it
// whether or not the field has focus. An empty field with neither keeps the
// label on the first text line.
func TestTextAreaLabelAndPlaceholder(t *testing.T) {
	for _, variant := range []string{"filled", "outlined"} {
		for _, tc := range []struct {
			name, extra string
			state       func(*tree.TextArea)
			floated     bool
		}{
			{"empty", "", nil, false},
			{"text", `text: "alpha"`, nil, true},
			{"placeholder", `placeholder: "Write a note"`, nil, true},
			{"focused", "", func(a *tree.TextArea) { a.Focused = true }, true},
		} {
			t.Run(variant+"/"+tc.name, func(t *testing.T) {
				src := `Window { Column { TextArea { id: a variant: ` + variant + ` label: "Notes" ` + tc.extra + ` width: 300 height: 140 } } }`
				l, _, box := textAreaPaint(t, src, tc.state)
				op, ok := textOps(l, "Notes")["Notes"]
				if !ok {
					t.Fatal("no label")
				}
				size := float64(op.FontSize)
				lh := l.style("bodyLarge").LineHeight
				want := box.Y + 34 + lh/2 + size*0.342
				if variant == "outlined" {
					want = box.Y + 16 + lh/2 + size*0.342
				}
				if tc.floated {
					top := box.Y + 16
					if variant == "outlined" {
						top = box.Y
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

// TestTextAreaPlaceholderSitsUnderTheLabel: both variants show the placeholder
// while the label is up, on the text area's first line and not behind it.
func TestTextAreaPlaceholderSitsUnderTheLabel(t *testing.T) {
	for _, variant := range []string{"filled", "outlined"} {
		src := `Window { Column { TextArea { id: a variant: ` + variant + ` label: "Notes" placeholder: "Write a note" width: 300 height: 140 } } }`
		l, _, _ := textAreaPaint(t, src, nil)
		found := textOps(l, "Notes", "Write a note")
		label, ok := found["Notes"]
		if !ok {
			t.Fatal("no label")
		}
		hint, ok := found["Write a note"]
		if !ok {
			t.Fatal("no placeholder")
		}
		if hint.TextY <= label.TextY {
			t.Fatalf("%s: placeholder at %g, label at %g", variant, hint.TextY, label.TextY)
		}
	}
}

// TestTextAreaOutlinedLabelCutsTheBorder: the top line stops at each side of
// the floated label, and the value starts at the outlined top padding.
func TestTextAreaOutlinedLabelCutsTheBorder(t *testing.T) {
	l, _, box := textAreaPaint(t, `Window {
		Column { TextArea { id: a variant: outlined label: "Notes" text: "alpha" width: 300 height: 140 } }
	}`, nil)
	op := textOps(l, "Notes")["Notes"]
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
		ink := float64(op.TextX) + float64(len([]rune(op.Text)))*float64(op.FontSize)*0.55/2
		if float64(o.Clip[0]+o.Clip[2]) <= ink {
			left = true
		}
		if float64(o.Clip[0]) >= ink {
			right = true
		}
	}
	if !top || !left || !right {
		t.Fatalf("outline top=%v left=%v right=%v:\n%s", top, left, right, l.Dump())
	}
	if value := textOps(l, "alpha")["alpha"]; float64(value.TextY) < box.Y+16 {
		t.Fatalf("value baseline = %v, want it below the outlined top padding", value.TextY)
	}
}
