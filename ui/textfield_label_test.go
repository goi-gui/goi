package ui

import "testing"

// TestTextFieldLabelAnimates: the label eases between the field and the top,
// starting no travel on its own, finishing exactly on the target and turning
// around when focus leaves mid-way.
func TestTextFieldLabelAnimates(t *testing.T) {
	const src = `Window { Column { TextField { id: f label: "Name" } } }`
	_, win, in := inputFixture(t, src, 480, 320)
	tf := MustByID[*TextField](win, "f")

	if in.tickFieldLabels(1.0 / 60) {
		t.Fatal("an untouched field is already animating")
	}
	if tf.LabelFloat != 0 || !tf.LabelPosed {
		t.Fatalf("resting label = %v posed %v, want 0/true", tf.LabelFloat, tf.LabelPosed)
	}

	tf.Focused = true
	if !in.tickFieldLabels(1.0 / 60) {
		t.Fatal("focus did not start the label")
	}
	if tf.LabelFloat <= 0 || tf.LabelFloat >= 1 {
		t.Fatalf("label = %v after one frame, want it in flight", tf.LabelFloat)
	}
	mid := tf.LabelFloat

	frames := 1
	for in.tickFieldLabels(1.0 / 60) {
		frames++
		if frames > 200 {
			t.Fatal("the label never reached the top")
		}
		if tf.LabelFloat < mid {
			t.Fatal("the label went backwards")
		}
	}
	if frames < 2 {
		t.Fatalf("landed in %d frames, that is not an animation", frames)
	}
	if tf.LabelFloat != 1 {
		t.Fatalf("final float = %v, want exactly 1", tf.LabelFloat)
	}

	// Blur turns it around instead of restarting from the top.
	tf.Focused = false
	in.tickFieldLabels(1.0 / 60)
	if tf.LabelFloat >= 1 || tf.LabelFloat <= mid {
		t.Fatalf("float after blur = %v, want it heading back", tf.LabelFloat)
	}
	for in.tickFieldLabels(1.0 / 60) {
	}
	if tf.LabelFloat != 0 {
		t.Fatalf("final float = %v, want exactly 0", tf.LabelFloat)
	}
}

// TestTextFieldLabelStartsAtRest: loading a page adopts the design's state on
// the first tick, whether the label belongs up (content or placeholder) or
// inside (an empty field), instead of sliding every label into place.
func TestTextFieldLabelStartsAtRest(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want float64
	}{
		{"empty", `label: "Name"`, 0},
		{"text", `label: "Name" text: "Ada"`, 1},
		{"placeholder", `label: "Name" placeholder: "you@example.com"`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `Window { Column { TextField { id: f ` + tc.body + ` } } }`
			_, win, in := inputFixture(t, src, 480, 320)
			tf := MustByID[*TextField](win, "f")
			if in.tickFieldLabels(1.0 / 60) {
				t.Fatal("the first frame animated a loaded field")
			}
			if tf.LabelFloat != tc.want || !tf.LabelPosed {
				t.Fatalf("float = %v posed %v, want %v/true", tf.LabelFloat, tf.LabelPosed, tc.want)
			}
		})
	}
}
