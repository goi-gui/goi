package ui

import (
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

const pickerUI = `Window {
	Column {
		padding: 0
		DatePicker { id: date width: 328 height: 400 }
		TimePicker { id: time width: 328 height: 420 }
	}
}`

// A click on a day cell must become the picker's value, and the arrows must move
// the month without touching it.
func TestDatePickerClick(t *testing.T) {
	_, win, in := inputFixture(t, pickerUI, 480, 900)
	dp := MustByID[*DatePicker](win, "date")
	var submitted []string
	dp.OnSubmit(func(v string) { submitted = append(submitted, v) })

	r := absoluteRect(dp)
	layout := paint.DateLayoutOf(r, "", 0)
	// The 15th of the shown month is in row 2 or 3; find its cell centre.
	iso, ok := layout.DateAt(
		layout.Grid.X+float64((14+layout.First)%7)*layout.Cell+layout.Cell/2,
		layout.Grid.Y+float64((14+layout.First)/7)*layout.CellH+layout.CellH/2,
	)
	if !ok {
		t.Fatal("15th has no cell")
	}
	x := layout.Grid.X + float64((14+layout.First)%7)*layout.Cell + layout.Cell/2
	y := layout.Grid.Y + float64((14+layout.First)/7)*layout.CellH + layout.CellH/2

	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if dp.Text.Get() != iso {
		t.Fatalf("value = %q, want %q", dp.Text.Get(), iso)
	}
	if len(submitted) != 1 || submitted[0] != iso {
		t.Fatalf("submitted %v, want [%s]", submitted, iso)
	}

	// An arrow moves the month and leaves the value alone.
	in.MouseDown(layout.Next.X+16, layout.Next.Y+16, 0, false)
	in.MouseUp(layout.Next.X+16, layout.Next.Y+16, 0)
	if dp.PickerMonth != layout.Month+1 {
		t.Fatalf("month = %d, want %d", dp.PickerMonth, layout.Month+1)
	}
	if dp.Text.Get() != iso {
		t.Fatalf("arrow changed the value to %q", dp.Text.Get())
	}
}

// The dial sets the hour, then hands over to the minutes, so two clicks make a
// complete time. The dial is a 12-hour one; AM/PM decides the half of the day.
func TestTimePickerClick(t *testing.T) {
	_, win, in := inputFixture(t, pickerUI, 480, 900)
	tp := MustByID[*TimePicker](win, "time")
	tp.Text.Set("10:30")
	r := absoluteRect(tp)
	layout := paint.TimeLayoutOf(r, "10:30", 0, "dial")

	// Click at three o'clock on the dial: hour 3, and the minutes become active.
	x := layout.Dial.X + layout.Dial.W/2 + layout.DialRadius()
	y := layout.Dial.Y + layout.Dial.H/2
	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if got := tp.Text.Get(); got != "03:30" {
		t.Fatalf("after hour click value = %q, want 03:30", got)
	}
	if tp.PickerField != 1 {
		t.Fatalf("field = %d, want the minutes", tp.PickerField)
	}

	// Six o'clock on the minutes dial is :30.
	in.MouseDown(layout.Dial.X+layout.Dial.W/2, layout.Dial.Y+layout.Dial.H/2+layout.DialRadius(), 0, false)
	in.MouseUp(layout.Dial.X+layout.Dial.W/2, layout.Dial.Y+layout.Dial.H/2+layout.DialRadius(), 0)
	if got := tp.Text.Get(); got != "03:30" {
		t.Fatalf("minutes click value = %q, want 03:30", got)
	}

	// The header halves switch the dial back to the hour.
	in.MouseDown(layout.HourBox.X+10, layout.HourBox.Y+10, 0, false)
	in.MouseUp(layout.HourBox.X+10, layout.HourBox.Y+10, 0)
	if tp.PickerField != 0 {
		t.Fatalf("field = %d, want the hour after a header click", tp.PickerField)
	}
}

// The dial ring must be a full circle: every position maps to its own hour, and
// the AM/PM state decides which half of the day it lands in.
func TestTimeLayoutDialPositions(t *testing.T) {
	r := tree.Rect{W: 328, H: 420}
	tl := paint.TimeLayoutOf(r, "12:00", 0, "dial") // noon, so PM
	cx, cy := tl.Dial.X+tl.Dial.W/2, tl.Dial.Y+tl.Dial.H/2
	radius := tl.DialRadius()
	seen := map[int]bool{}
	for i := 0; i < 12; i++ {
		ang := float64(i) * math.Pi / 6
		x := cx + radius*math.Sin(ang)
		y := cy - radius*math.Cos(ang)
		v, _, ok := tl.TimeAt(x, y)
		if !ok {
			t.Fatalf("position %d missed the dial", i)
		}
		if seen[i] {
			t.Fatalf("position %d hit twice", i)
		}
		seen[i] = true
		h := i
		if h == 0 {
			h = 12
		}
		want := h
		if want != 12 {
			want += 12 // PM
		}
		if v != clockText(want, 0) {
			t.Fatalf("position %d = %q, want %s", i, v, clockText(want, 0))
		}
	}
}

// The AM/PM switch keeps the 12-hour reading and moves the 24-hour value.
func TestTimePickerHalfOfDay(t *testing.T) {
	_, win, in := inputFixture(t, pickerUI, 480, 900)
	tp := MustByID[*TimePicker](win, "time")
	tp.Text.Set("10:30")
	layout := paint.TimeLayoutOf(absoluteRect(tp), "10:30", 0, "dial")

	click := func(r tree.Rect) {
		in.MouseDown(r.X+r.W/2, r.Y+r.H/2, 0, false)
		in.MouseUp(r.X+r.W/2, r.Y+r.H/2, 0)
	}
	click(layout.PMBox)
	if got := tp.Text.Get(); got != "22:30" {
		t.Fatalf("after PM = %q, want 22:30", got)
	}
	click(layout.AMBox)
	if got := tp.Text.Get(); got != "10:30" {
		t.Fatalf("after AM = %q, want 10:30", got)
	}
	// Midnight and noon are the two edge cases.
	tp.Text.Set("00:05")
	tl := paint.TimeLayoutOf(absoluteRect(tp), "00:05", 0, "dial")
	click(tl.PMBox)
	if got := tp.Text.Get(); got != "12:05" {
		t.Fatalf("midnight to PM = %q, want 12:05", got)
	}
}

// The corner icon swaps the clock for the text fields and back.
func TestTimePickerVariantSwitch(t *testing.T) {
	_, win, in := inputFixture(t, pickerUI, 480, 900)
	tp := MustByID[*TimePicker](win, "time")
	tp.Text.Set("08:15")
	layout := paint.TimeLayoutOf(absoluteRect(tp), "08:15", 0, "dial")
	in.MouseDown(layout.Switch.X+10, layout.Switch.Y+10, 0, false)
	in.MouseUp(layout.Switch.X+10, layout.Switch.Y+10, 0)
	if got := tp.Variant.Get(); got != "input" {
		t.Fatalf("variant = %q, want input", got)
	}
	// The two variants have different heights, so the switch must lay out again;
	// a repaint alone left the card at the old size and the picture broke.
	if kind, dirty := tp.Base().Dirty(); !dirty || kind != tree.DirtyLayout {
		t.Fatalf("the variant switch marked dirty=%v kind=%v, want a layout change", dirty, kind)
	}
	inLayout := paint.TimeLayoutOf(absoluteRect(tp), "08:15", 0, "input")
	in.MouseDown(inLayout.Switch.X+10, inLayout.Switch.Y+10, 0, false)
	in.MouseUp(inLayout.Switch.X+10, inLayout.Switch.Y+10, 0)
	if got := tp.Variant.Get(); got != "dial" {
		t.Fatalf("variant = %q, want dial", got)
	}
}

// The OK button submits and Cancel cancels, both with the current value.
func TestTimePickerActions(t *testing.T) {
	_, win, in := inputFixture(t, pickerUI, 480, 900)
	tp := MustByID[*TimePicker](win, "time")
	tp.Text.Set("06:45")
	var submitted, cancelled, confirmed []string
	tp.OnSubmit(func(v string) { submitted = append(submitted, v) })
	tp.OnCancel(func(v string) { cancelled = append(cancelled, v) })
	tp.OnOK(func(v string) { confirmed = append(confirmed, v) })
	layout := paint.TimeLayoutOf(absoluteRect(tp), "06:45", 0, "dial")

	click := func(r tree.Rect) {
		in.MouseDown(r.X+r.W/2, r.Y+r.H/2, 0, false)
		in.MouseUp(r.X+r.W/2, r.Y+r.H/2, 0)
	}
	click(layout.OK)
	click(layout.Cancel)
	if len(submitted) != 1 || submitted[0] != "06:45" {
		t.Fatalf("submitted = %v, want [06:45]", submitted)
	}
	if len(cancelled) != 1 || cancelled[0] != "06:45" {
		t.Fatalf("cancelled = %v, want [06:45]", cancelled)
	}
	if len(confirmed) != 1 || confirmed[0] != "06:45" {
		t.Fatalf("onOK = %v, want [06:45]", confirmed)
	}
}

// In the input variant a field takes typed digits: one starts the number, a
// second completes it and hands the hour over to the minutes.
func TestTimePickerTyping(t *testing.T) {
	_, win, in := inputFixture(t, pickerUI, 480, 900)
	tp := MustByID[*TimePicker](win, "time")
	tp.Variant.Set("input")
	tp.Text.Set("00:00")
	r := absoluteRect(tp)

	// Press the hour box: it takes focus, so the interactor routes keys here.
	layout := paint.TimeLayoutOf(r, "00:00", 0, "input")
	in.MouseDown(layout.HourBox.X+10, layout.HourBox.Y+10, 0, false)
	in.MouseUp(layout.HourBox.X+10, layout.HourBox.Y+10, 0)
	if in.kbd != tp.Base().Node() {
		t.Fatal("the picker did not take keyboard focus")
	}
	// Digits arrive as committed text from the platform, not as key presses.
	in.CommitText("0")
	in.CommitText("7")
	if got := tp.Text.Get(); got != "07:00" {
		t.Fatalf("typed hour = %q, want 07:00", got)
	}
	if tp.PickerField != 1 {
		t.Fatalf("field = %d, want the minutes after two digits", tp.PickerField)
	}
	in.CommitText("4")
	in.CommitText("5")
	if got := tp.Text.Get(); got != "07:45" {
		t.Fatalf("typed minutes = %q, want 07:45", got)
	}
	// Enter is OK: it reports through onSubmit and onOK alike.
	var submitted, confirmed []string
	tp.OnSubmit(func(v string) { submitted = append(submitted, v) })
	tp.OnOK(func(v string) { confirmed = append(confirmed, v) })
	in.key(tree.KeyEnter, tree.Mods{})
	if len(submitted) != 1 || submitted[0] != "07:45" {
		t.Fatalf("submitted = %v, want [07:45]", submitted)
	}
	if len(confirmed) != 1 || confirmed[0] != "07:45" {
		t.Fatalf("onOK = %v, want [07:45]", confirmed)
	}
}
