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
	layout := paint.TimeLayoutOf(r, "10:30", 0, "dial", 12)

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
	// Picking minutes keeps the minutes active: the hour never comes back on
	// its own.
	if tp.PickerField != 1 {
		t.Fatalf("field = %d after picking minutes, want the minutes", tp.PickerField)
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
	tl := paint.TimeLayoutOf(r, "12:00", 0, "dial", 12) // noon, so PM
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
	layout := paint.TimeLayoutOf(absoluteRect(tp), "10:30", 0, "dial", 12)

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
	tl := paint.TimeLayoutOf(absoluteRect(tp), "00:05", 0, "dial", 12)
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
	layout := paint.TimeLayoutOf(absoluteRect(tp), "08:15", 0, "dial", 12)
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
	inLayout := paint.TimeLayoutOf(absoluteRect(tp), "08:15", 0, "input", 12)
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
	layout := paint.TimeLayoutOf(absoluteRect(tp), "06:45", 0, "dial", 12)

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
	layout := paint.TimeLayoutOf(r, "00:00", 0, "input", 12)
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

// TestTimePicker24Hour: hourFormat 24 drops the AM/PM switch, spells the hour
// 00-23, and turns the dial into the two-ring 24-hour one.
func TestTimePicker24Hour(t *testing.T) {
	_, win, in := inputFixture(t, pickerUI, 480, 900)
	tp := MustByID[*TimePicker](win, "time")
	tp.Text.Set("15:30")
	tp.HourFormat.Set(24)
	r := absoluteRect(tp)
	tl := paint.TimeLayoutOf(r, "15:30", 0, "dial", 24)
	if tl.AMBox.W != 0 || tl.PMBox.W != 0 {
		t.Fatal("a 24-hour picker still laid out the AM/PM switch")
	}
	if got := tl.HourText(); got != "15" {
		t.Fatalf("hour text = %q, want 15", got)
	}
	cx, cy := tl.Dial.X+tl.Dial.W/2, tl.Dial.Y+tl.Dial.H/2
	at := func(radius float64, hour int) string {
		ang := float64(hour%12) * math.Pi / 6
		v, _, _ := tl.TimeAt(cx+radius*math.Sin(ang), cy-radius*math.Cos(ang))
		return v
	}
	if got := at(tl.DialRadius(), 3); got != "03:30" {
		t.Fatalf("outer 3 = %q, want 03:30", got)
	}
	if got := at(tl.InnerRadius(), 3); got != "15:30" {
		t.Fatalf("inner 3 = %q, want 15:30", got)
	}
	if got := at(tl.InnerRadius(), 0); got != "00:30" {
		t.Fatalf("inner 0 = %q, want 00:30", got)
	}
	if got := at(tl.DialRadius(), 0); got != "12:30" {
		t.Fatalf("outer 0 = %q, want 12:30", got)
	}

	// The format chip on the action row flips the clock without touching the
	// value.
	in.MouseDown(tl.FormatToggle.X+tl.FormatToggle.W/2, tl.FormatToggle.Y+tl.FormatToggle.H/2, 0, false)
	in.MouseUp(tl.FormatToggle.X+tl.FormatToggle.W/2, tl.FormatToggle.Y+tl.FormatToggle.H/2, 0)
	if got := tp.HourFormat.Get(); got != 12 {
		t.Fatalf("hourFormat = %v, want 12 after the chip", got)
	}
	if got := tp.Text.Get(); got != "15:30" {
		t.Fatalf("the format switch changed the value to %q", got)
	}
}

// TestTimePicker24HourDialMarksTheInnerRing: 15:30 highlights 15 on the inner
// ring, and midnight does too.
func TestTimePicker24HourDialMarksTheInnerRing(t *testing.T) {
	for _, c := range []struct {
		value string
		inner bool
		index int
	}{
		{"15:30", true, 3},
		{"00:10", true, 0},
		{"23:59", true, 11},
		{"09:00", false, 9},
		{"12:00", false, 0},
	} {
		tl := paint.TimeLayoutOf(tree.Rect{W: 328, H: 420}, c.value, 0, "dial", 24)
		if got := tl.DialInner(); got != c.inner || tl.DialIndex() != c.index {
			t.Errorf("%s: inner=%v index=%d, want %v/%d", c.value, got, tl.DialIndex(), c.inner, c.index)
		}
	}
}

// TestTimePickerMinuteDrag: the minute ring has sixty positions, so dragging
// the hand picks 13 as well as 15, and the field stays on the minutes.
func TestTimePickerMinuteDrag(t *testing.T) {
	_, win, in := inputFixture(t, pickerUI, 480, 900)
	tp := MustByID[*TimePicker](win, "time")
	tp.Text.Set("10:30")
	r := absoluteRect(tp)
	tl := paint.TimeLayoutOf(r, "10:30", 0, "dial", 12)
	cx, cy := tl.Dial.X+tl.Dial.W/2, tl.Dial.Y+tl.Dial.H/2
	at := func(minute float64) (float64, float64) {
		ang := minute / 60 * 2 * math.Pi
		return cx + tl.DialRadius()*math.Sin(ang), cy - tl.DialRadius()*math.Cos(ang)
	}

	// Pick the hour first, which hands over to the minutes.
	x3, y3 := at(15) // three o'clock, 15 minutes
	in.MouseDown(x3, y3, 0, false)
	in.MouseUp(x3, y3, 0)
	if got := tp.Text.Get(); got != "03:30" || tp.PickerField != 1 {
		t.Fatalf("hour pick = %q field %d", got, tp.PickerField)
	}

	// Drag from the 13th minute to the 47th: no label sits on either. A dial
	// change submits, but it is not a confirmation, so onOK stays quiet.
	oks := 0
	tp.OnOK(func(string) { oks++ })
	x13, y13 := at(13)
	x47, y47 := at(47)
	in.MouseDown(x13, y13, 0, false)
	if got := tp.Text.Get(); got != "03:13" {
		t.Fatalf("down at minute 13 = %q", got)
	}
	in.MouseMove(x47, y47)
	if got := tp.Text.Get(); got != "03:47" {
		t.Fatalf("drag to minute 47 = %q", got)
	}
	in.MouseUp(x47, y47, 0)
	if got := tp.Text.Get(); got != "03:47" {
		t.Fatalf("release = %q", got)
	}
	if tp.PickerField != 1 {
		t.Fatalf("field = %d after dragging minutes, want the minutes", tp.PickerField)
	}
	if oks != 0 {
		t.Fatalf("a dial drag fired onOK %d times", oks)
	}

	// A plain click on a minute leaves the minutes active too.
	x5, y5 := at(5)
	in.MouseDown(x5, y5, 0, false)
	in.MouseUp(x5, y5, 0)
	if got := tp.Text.Get(); got != "03:05" || tp.PickerField != 1 {
		t.Fatalf("minute click = %q field %d", got, tp.PickerField)
	}
}
