package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

func timePickerList(t *testing.T, variant string) *List {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		TimePicker { width: 328 height: 420 variant: `+variant+` text: "07:00" }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 480, 900)
	l := &List{}
	l.Build(tr, 480, 900)
	return l
}

// TestTimePickerDialPaintsTheClock is the picture: a 12-hour ring with a hand
// and the picked number filled, the selected field and AM/PM tinted, and the
// keyboard switch that leads to the fields.
func TestTimePickerDialPaintsTheClock(t *testing.T) {
	l := timePickerList(t, "dial")
	col := m3.Light().Colors
	texts := map[string]bool{}
	for _, op := range l.Ops {
		if op.Kind == Text && !op.Icon {
			texts[op.Text] = true
		}
	}
	for _, want := range []string{"Select time", "12", "11", "7", "6", "AM", "PM", "Cancel", "OK"} {
		if !texts[want] {
			t.Fatalf("dial missing %q: %v", want, texts)
		}
	}
	// The inner 24-hour ring is gone; "00" here is the minute field.
	if texts["13"] || texts["23"] {
		t.Fatal("the dial still draws the 24-hour inner ring")
	}
	if !hasIcon(l, "keyboard") {
		t.Fatal("the dial has no keyboard switch")
	}
	hand, picked, am := false, false, false
	for _, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		if op.Rotate != 0 && op.Rect[3] == 2 {
			hand = true
		}
		if op.Shape == Circle && op.Color == col.Primary && op.Rect[2] == 36 {
			picked = true
		}
		if op.Color == col.TertiaryContainer {
			am = true
		}
	}
	if !hand || !picked || !am {
		t.Fatalf("dial parts: hand=%v picked=%v ampm=%v", hand, picked, am)
	}
}

// TestTimePickerInputPaintsFields: no clock, the two labelled fields with a
// caret in the active one, and the clock switch back.
func TestTimePickerInputPaintsFields(t *testing.T) {
	l := timePickerList(t, "input")
	col := m3.Light().Colors
	texts := map[string]bool{}
	caret := false
	for _, op := range l.Ops {
		if op.Kind == Text && !op.Icon {
			texts[op.Text] = true
		}
		if op.Kind == Fill && op.Rect[2] == 2 && op.Rect[3] == 40 && op.Color == col.Primary {
			caret = true
		}
	}
	for _, want := range []string{"Enter time", "07", "00", "Hour", "Minute", "AM", "PM", "Cancel", "OK"} {
		if !texts[want] {
			t.Fatalf("input missing %q: %v", want, texts)
		}
	}
	if texts["12"] {
		t.Fatal("the input variant drew the clock")
	}
	if !hasIcon(l, "schedule") {
		t.Fatal("the input variant has no clock switch")
	}
	if !caret {
		t.Fatal("the active field has no caret")
	}
}

func hasIcon(l *List, name string) bool {
	for _, op := range l.Ops {
		if op.Kind == Text && op.Icon && op.Text == name {
			return true
		}
	}
	return false
}
