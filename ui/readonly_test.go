package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

// TestReadOnlyBlocksToggles keeps checked values where the design put them: a
// press still clicks, but it does not flip the control.
func TestReadOnlyBlocksToggles(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Column {
		Checkbox { id: cb width: 200 height: 48 readOnly: true checked: true }
		Switch { id: sw width: 200 height: 48 readOnly: true checked: true }
		RadioButton { id: rb width: 200 height: 48 readOnly: true checked: true }
	} }`, 480, 320)
	for _, id := range []string{"cb", "sw", "rb"} {
		n, ok := win.tree.ByID(id)
		if !ok {
			t.Fatalf("%s not found", id)
		}
		b := absoluteRect(n)
		in.MouseDown(b.X+b.W/2, b.Y+b.H/2, 0, false)
		in.MouseUp(b.X+b.W/2, b.Y+b.H/2, 0)
		if c := comp(n); c != nil && !c.Checked.Get() {
			t.Fatalf("%s changed under a read-only press", id)
		}
	}
}

// TestReadOnlyBlocksTextEdits covers the three text inputs: typing, deleting,
// pasting and an IME commit leave the value alone.
func TestReadOnlyBlocksTextEdits(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Column {
		TextField { id: field width: 220 height: 56 text: "keep" readOnly: true }
		TextArea { id: area width: 220 height: 80 text: "keep" readOnly: true }
		SearchBar { id: bar width: 220 height: 56 text: "keep" readOnly: true }
	} }`, 480, 400)
	for _, id := range []string{"field", "area", "bar"} {
		n, ok := win.tree.ByID(id)
		if !ok {
			t.Fatalf("%s not found", id)
		}
		b := absoluteRect(n)
		in.MouseDown(b.X+16, b.Y+10, 0, false)
		in.MouseUp(b.X+16, b.Y+10, 0)
		in.Char('x')
		in.key(tree.KeyBackspace, tree.Mods{})
		in.key(tree.KeyV, tree.Mods{Ctrl: true})
		in.CommitText("abc")
		if got, _ := n.Base().PropValue("text"); got != "keep" {
			t.Fatalf("%s = %v, want keep", id, got)
		}
	}
}

// TestReadOnlyBlocksSlider keeps both the keys and the pointer off a read-only
// slider.
func TestReadOnlyBlocksSlider(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Column {
		Slider { id: s width: 240 height: 40 value: 0.5 min: 0 max: 1 readOnly: true }
	} }`, 480, 320)
	s := MustByID[*Slider](win, "s")
	in.setKbd(s.Base().Node())
	in.key(tree.KeyRight, tree.Mods{})
	if got := s.Value.Get(); got != 0.5 {
		t.Fatalf("key changed the value to %v", got)
	}
	b := absoluteRect(s)
	in.MouseDown(b.X+b.W*0.9, b.Y+b.H/2, 0, false)
	in.MouseUp(b.X+b.W*0.9, b.Y+b.H/2, 0)
	if got := s.Value.Get(); got != 0.5 {
		t.Fatalf("press changed the value to %v", got)
	}
}

// TestReadOnlyBlocksPickers covers the three picker surfaces: a day click, a
// dial key and a colour drag all leave the value as the design set it.
func TestReadOnlyBlocksPickers(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Column {
		DatePicker { id: date width: 328 height: 400 readOnly: true text: "2026-11-02" }
		TimePicker { id: time width: 328 height: 420 readOnly: true text: "07:00" }
		ColorPicker { id: color width: 328 height: 300 readOnly: true text: "#6750a4" }
	} }`, 480, 1200)

	dp := MustByID[*DatePicker](win, "date")
	submitted := 0
	dp.OnSubmit(func(string) { submitted++ })
	dl := paint.DateLayoutOf(absoluteRect(dp), dp.Text.Get(), dp.PickerMonth)
	iso, ok := dl.DateAt(
		dl.Grid.X+float64((14+dl.First)%7)*dl.Cell+dl.Cell/2,
		dl.Grid.Y+float64((14+dl.First)/7)*dl.CellH+dl.CellH/2,
	)
	if !ok {
		t.Fatal("15th has no cell")
	}
	x := dl.Grid.X + float64((14+dl.First)%7)*dl.Cell + dl.Cell/2
	y := dl.Grid.Y + float64((14+dl.First)/7)*dl.CellH + dl.CellH/2
	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if got := dp.Text.Get(); got != "2026-11-02" || got == iso {
		t.Fatalf("date = %q, want 2026-11-02", got)
	}
	if submitted != 0 {
		t.Fatalf("submits = %d, want none", submitted)
	}

	tp := MustByID[*TimePicker](win, "time")
	in.setKbd(tp.Base().Node())
	in.key(tree.KeyUp, tree.Mods{})
	in.Char('9')
	if got := tp.Text.Get(); got != "07:00" {
		t.Fatalf("time = %q, want 07:00", got)
	}

	cp := MustByID[*ColorPicker](win, "color")
	cl := paint.ColorLayoutOf(absoluteRect(cp))
	in.MouseDown(cl.Hue.X+cl.Hue.W/2, cl.Hue.Y+cl.Hue.H/2, 0, false)
	in.MouseUp(cl.Hue.X+cl.Hue.W/2, cl.Hue.Y+cl.Hue.H/2, 0)
	in.setKbd(cp.Base().Node())
	in.key(tree.KeyLeft, tree.Mods{})
	if got := cp.Text.Get(); got != "#6750a4" {
		t.Fatalf("colour = %q, want #6750a4", got)
	}
}
