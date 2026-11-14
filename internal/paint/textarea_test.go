package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

func textAreaList(t *testing.T, focused bool) *List {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		TextArea { label: "Notes" placeholder: "Write a note" width: 300 height: 140 }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if focused {
		for _, n := range tr.All() {
			if a, ok := n.(*tree.TextArea); ok {
				a.Focused = true
			}
		}
	}
	layout.New(nil).Layout(tr.Root, 400, 300)
	l := &List{}
	l.Build(tr, 400, 300)
	return l
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

// TestTextAreaLabelOrPlaceholder: an empty unfocused field shows one line, not
// the label under the placeholder; focus floats the label and leaves the
// placeholder on the text line below it.
func TestTextAreaLabelOrPlaceholder(t *testing.T) {
	l := textAreaList(t, false)
	if op, ok := textOps(l, "Notes", "Write a note")["Notes"]; !ok || op.TextY == 0 {
		t.Fatal("empty field does not show its label")
	}
	if _, ok := textOps(l, "Write a note")["Write a note"]; ok {
		t.Fatal("empty unfocused field drew the placeholder over the label")
	}

	l = textAreaList(t, true)
	found := textOps(l, "Notes", "Write a note")
	label, ok := found["Notes"]
	if !ok {
		t.Fatal("focused field lost its floating label")
	}
	placeholder, ok := found["Write a note"]
	if !ok {
		t.Fatal("focused empty field lost its placeholder")
	}
	if placeholder.TextY <= label.TextY {
		t.Fatalf("placeholder at %g, label at %g: the two overlap", placeholder.TextY, label.TextY)
	}
}
