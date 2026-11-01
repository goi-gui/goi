package ui

import (
	"math"
	"testing"

	"github.com/ksckaan1/goi/internal/paint"
	"github.com/ksckaan1/goi/internal/tree"
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
// complete time.
func TestTimePickerClick(t *testing.T) {
	_, win, in := inputFixture(t, pickerUI, 480, 900)
	tp := MustByID[*TimePicker](win, "time")
	tp.Text.Set("10:30")
	r := absoluteRect(tp)
	layout := paint.TimeLayoutOf(r, "10:30", 0)

	// Click at three o'clock on the dial: hour 3, and the minutes become active.
	x := layout.Dial.X + layout.Dial.W/2 + layout.Dial.W/2 - 28
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
	in.MouseDown(layout.Dial.X+layout.Dial.W/2, layout.Dial.Y+layout.Dial.H-28, 0, false)
	in.MouseUp(layout.Dial.X+layout.Dial.W/2, layout.Dial.Y+layout.Dial.H-28, 0)
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

// The dial ring must be a full circle: every position maps to its own label.
func TestTimeLayoutDialPositions(t *testing.T) {
	r := tree.Rect{W: 328, H: 420}
	tl := paint.TimeLayoutOf(r, "12:00", 0)
	cx, cy := tl.Dial.X+tl.Dial.W/2, tl.Dial.Y+tl.Dial.H/2
	radius := tl.Dial.W/2 - 28
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
		_ = v
	}
}

// A 24-hour dial needs both rings: outside is 1-12, inside is 13-00.
func TestTimeLayout24HourRings(t *testing.T) {
	tl := paint.TimeLayoutOf(tree.Rect{W: 328, H: 420}, "09:15", 0)
	cx, cy := tl.Dial.X+tl.Dial.W/2, tl.Dial.Y+tl.Dial.H/2
	at := func(radius float64, hour int) (string, int, bool) {
		ang := float64(hour%12) * math.Pi / 6
		return tl.TimeAt(cx+radius*math.Sin(ang), cy-radius*math.Cos(ang))
	}

	// Three o'clock outside is 03, inside is 15.
	if v, _, _ := at(tl.OuterRadius(), 3); v != "03:15" {
		t.Fatalf("outer 3 = %q, want 03:15", v)
	}
	if v, _, _ := at(tl.InnerRadius(), 3); v != "15:15" {
		t.Fatalf("inner 3 = %q, want 15:15", v)
	}
	// The top of the inner ring is 00, not 24 or 12.
	if v, _, _ := at(tl.InnerRadius(), 0); v != "00:15" {
		t.Fatalf("inner 0 = %q, want 00:15", v)
	}
	if v, _, _ := at(tl.OuterRadius(), 0); v != "12:15" {
		t.Fatalf("outer 0 = %q, want 12:15", v)
	}
}

// The selected hour must highlight on the ring that holds it.
func TestTimeLayoutDialInner(t *testing.T) {
	for _, c := range []struct {
		value string
		inner bool
	}{{"09:00", false}, {"12:00", false}, {"13:00", true}, {"00:30", true}, {"23:59", true}} {
		tl := paint.TimeLayoutOf(tree.Rect{W: 328, H: 420}, c.value, 0)
		if got := tl.DialInner(); got != c.inner {
			t.Errorf("%s: DialInner = %v, want %v", c.value, got, c.inner)
		}
	}
}
