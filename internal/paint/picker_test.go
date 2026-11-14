package paint

import (
	"math"
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
	for _, op := range l.Ops {
		if op.Kind == Text && !op.Icon {
			texts[op.Text] = true
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
	for _, op := range l.Ops {
		// The active field is outlined, never marked with a caret over the
		// number: the border already says which field takes keys.
		if op.Kind == Fill && op.Rect[2] == 2 && op.Rect[3] == 40 && op.Color == col.Primary {
			t.Fatal("the active field drew a caret")
		}
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

// TestTimePicker24HourPaint: a 24-hour dial labels both rings, the AM/PM switch
// is gone, and the action row carries the `24h` chip.
func TestTimePicker24HourPaint(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		TimePicker { width: 328 height: 420 variant: dial hourFormat: 24 text: "19:30" }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 480, 900)
	l := &List{}
	l.Build(tr, 480, 900)
	texts := map[string]bool{}
	chip := false
	for _, op := range l.Ops {
		if op.Kind == Text && !op.Icon {
			texts[op.Text] = true
		}
	}
	for _, want := range []string{"19", "13", "23", "00", "12", "24h"} {
		if !texts[want] {
			t.Fatalf("24-hour picker missing %q: %v", want, texts)
		}
	}
	if texts["AM"] || texts["PM"] {
		t.Fatal("the 24-hour picker still draws the AM/PM switch")
	}
	_ = chip
}

// TestTimePickerMinuteHandTakesEveryMinute: the picked minute rides the hand's
// own angle, so 13 shows its circle where 13 is, and the label it covers (15)
// steps aside.
func TestTimePickerMinuteHandTakesEveryMinute(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		TimePicker { width: 328 height: 420 variant: dial text: "03:13" }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	for _, n := range tr.All() {
		if tp, ok := n.(*tree.TimePicker); ok {
			tp.PickerField = 1
		}
	}
	layout.New(nil).Layout(tr.Root, 480, 900)
	l := &List{}
	l.Build(tr, 480, 900)
	tl := TimeLayoutOf(tree.Rect{X: 0, Y: 0, W: 328, H: 420}, "03:13", 1, "dial", 12)

	texts := map[string]bool{}
	var picked *Op
	for i := range l.Ops {
		op := &l.Ops[i]
		if op.Kind == Text && !op.Icon {
			texts[op.Text] = true
		}
		if op.Kind == Fill && op.Shape == Circle && op.Rect[2] == 36 {
			picked = op
		}
	}
	if !texts["13"] {
		t.Fatalf("the picked minute is not drawn: %v", texts)
	}
	if texts["15"] {
		t.Fatal("the label under the picked minute was not skipped")
	}
	if picked == nil {
		t.Fatal("no picked-minute circle")
	}
	// The circle sits at 13/60 of a turn from twelve o'clock.
	cx, cy := tl.Dial.X+tl.Dial.W/2, tl.Dial.Y+tl.Dial.H/2
	gotX := float64(picked.Rect[0]) + float64(picked.Rect[2])/2
	gotY := float64(picked.Rect[1]) + float64(picked.Rect[3])/2
	wantAngle := 13.0 / 60 * 2 * math.Pi
	gotAngle := math.Atan2(gotX-cx, cy-gotY)
	if math.Abs(gotAngle-wantAngle) > 0.02 {
		t.Fatalf("picked circle at angle %v, want %v", gotAngle, wantAngle)
	}
}
