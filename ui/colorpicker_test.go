package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

const colorPickerUI = `Window {
	Column {
		padding: 10
		ColorPicker { id: color width: 328 height: 392 text: "#ff0000" }
	}
}`

// TestColorPickerPicksAndDrags: the square sets saturation and value, the hue
// bar sets the hue, a drag follows the pointer and the release reports once.
func TestColorPickerPicksAndDrags(t *testing.T) {
	_, win, in := inputFixture(t, colorPickerUI, 480, 400)
	cp := MustByID[*ColorPicker](win, "color")
	cl := paint.ColorLayoutOf(absoluteRect(cp))
	edits, submits := 0, 0
	var last string
	cp.OnEdit(func(v string) { edits++; last = v })
	cp.OnSubmit(func(v string) { submits++; last = v })

	// The hue bar's middle is 180 degrees, so red becomes cyan.
	hx := cl.Hue.X + cl.Hue.W/2
	hy := cl.Hue.Y + cl.Hue.H/2
	in.MouseDown(hx, hy, 0, false)
	in.MouseUp(hx, hy, 0)
	if got := cp.Text.Get(); got != "#00ffff" {
		t.Fatalf("hue pick = %q, want #00ffff", got)
	}

	// The square's centre is half saturation and half value: a muted cyan.
	sx := cl.SV.X + cl.SV.W/2
	sy := cl.SV.Y + cl.SV.H/2
	in.MouseDown(sx, sy, 0, false)
	in.MouseUp(sx, sy, 0)
	if got := cp.Text.Get(); got != "#408080" {
		t.Fatalf("square pick = %q, want #408080", got)
	}
	if edits != 2 {
		t.Fatalf("edits = %d, want 2", edits)
	}
	if submits != 2 || last != "#408080" {
		t.Fatalf("submits = %d last %q", submits, last)
	}
}

// TestColorPickerDragClampsAndKeepsHue: leaving the square keeps tracking its
// edge, and the hue survives a black value.
func TestColorPickerDragClampsAndKeepsHue(t *testing.T) {
	_, win, in := inputFixture(t, colorPickerUI, 480, 400)
	cp := MustByID[*ColorPicker](win, "color")
	cp.Text.Set("#00ff00")
	cl := paint.ColorLayoutOf(absoluteRect(cp))
	submits := 0
	cp.OnSubmit(func(string) { submits++ })

	// Press halfway down the left edge, then drag far past the bottom right:
	// saturation clamps to 1 and value to 0, which is black.
	in.MouseDown(cl.SV.X+1, cl.SV.Y+cl.SV.H/2, 0, false)
	in.MouseMove(cl.SV.Right()+200, cl.SV.Bottom()+200)
	in.MouseUp(cl.SV.Right()+200, cl.SV.Bottom()+200, 0)
	if got := cp.Text.Get(); got != "#000000" {
		t.Fatalf("drag = %q, want black", got)
	}
	if h, _, _ := cp.HSV(); h != 120 {
		t.Fatalf("hue after black = %v, want 120", h)
	}
	if submits != 1 {
		t.Fatalf("submits = %d, want one on release", submits)
	}
}

// TestColorPickerKeyboard: arrows edit, Ctrl changes saturation, Enter submits.
func TestColorPickerKeyboard(t *testing.T) {
	_, win, in := inputFixture(t, colorPickerUI, 480, 400)
	cp := MustByID[*ColorPicker](win, "color")
	cp.Text.Set("#ff0000")
	in.setKbd(cp)

	edits, submits := 0, 0
	cp.OnEdit(func(string) { edits++ })
	cp.OnSubmit(func(v string) { submits++ })

	before := cp.Text.Get()
	in.key(tree.KeyRight, noMods)
	if got := cp.Text.Get(); got == before || edits != 1 {
		t.Fatalf("arrow did not edit: %q edits %d", got, edits)
	}
	in.key(tree.KeyDown, tree.Mods{Ctrl: true})
	if edits != 2 {
		t.Fatalf("ctrl+down did not edit: %d", edits)
	}
	in.key(tree.KeyLeft, tree.Mods{Ctrl: true})
	if edits != 3 || cp.Alpha >= 1 {
		t.Fatalf("ctrl+left alpha = %v edits %d", cp.Alpha, edits)
	}
	in.key(tree.KeyEnter, noMods)
	if submits != 1 {
		t.Fatalf("Enter submits = %d", submits)
	}
}

// TestColorPickerHexInput types a colour into the field, reports it once it
// parses, and OK normalizes and submits it.
func TestColorPickerHexInput(t *testing.T) {
	_, win, in := inputFixture(t, colorPickerUI, 480, 450)
	cp := MustByID[*ColorPicker](win, "color")
	cl := paint.ColorLayoutOf(absoluteRect(cp))
	edits, submits, oks := 0, 0, 0
	var last string
	cp.OnEdit(func(v string) { edits++; last = v })
	cp.OnSubmit(func(v string) { submits++; last = v })
	cp.OnOK(func(v string) { oks++; last = v })

	// Focus the field, select the whole value and type a new one without the
	// leading hash.
	in.MouseDown(cl.Hex.X+cl.Hex.W/2, cl.Hex.Y+cl.Hex.H/2, 0, false)
	in.MouseUp(cl.Hex.X+cl.Hex.W/2, cl.Hex.Y+cl.Hex.H/2, 0)
	in.key(tree.KeyA, tree.Mods{Ctrl: true})
	for _, r := range "00ff00" {
		in.Char(r)
	}
	if got := cp.Text.Get(); got != "00ff00" {
		t.Fatalf("typed text = %q", got)
	}
	// The three-digit prefix parses too, so the last edit is the full colour.
	if edits == 0 || last != "#00ff00" {
		t.Fatalf("edits = %d last = %q", edits, last)
	}
	in.key(tree.KeyEnter, noMods)
	if got := cp.Text.Get(); got != "#00ff00" {
		t.Fatalf("Enter did not normalize: %q", got)
	}
	if submits != 1 || oks != 1 || last != "#00ff00" {
		t.Fatalf("submits = %d OKs = %d last = %q", submits, oks, last)
	}
}

// TestColorPickerActions: Cancel and OK report their own events, and OK also
// submits; a click on the field does not leak into an action.
func TestColorPickerActions(t *testing.T) {
	_, win, in := inputFixture(t, colorPickerUI, 480, 450)
	cp := MustByID[*ColorPicker](win, "color")
	cl := paint.ColorLayoutOf(absoluteRect(cp))
	cancels, submits, oks := 0, 0, 0
	var last string
	cp.OnCancel(func(v string) { cancels++; last = v })
	cp.OnSubmit(func(v string) { submits++; last = v })
	cp.OnOK(func(v string) { oks++; last = v })

	click := func(box tree.Rect) {
		x, y := box.X+box.W/2, box.Y+box.H/2
		in.MouseDown(x, y, 0, false)
		in.MouseUp(x, y, 0)
	}
	click(cl.OK)
	if oks != 1 || submits != 1 || last != "#ff0000" {
		t.Fatalf("OK = %d submits = %d last = %q", oks, submits, last)
	}
	click(cl.Cancel)
	if cancels != 1 || last != "#ff0000" {
		t.Fatalf("Cancel = %d last = %q", cancels, last)
	}
}

// TestColorPickerHexFilters: only hex digits and the hash reach the field.
func TestColorPickerHexFilters(t *testing.T) {
	_, win, in := inputFixture(t, colorPickerUI, 480, 450)
	cp := MustByID[*ColorPicker](win, "color")
	cl := paint.ColorLayoutOf(absoluteRect(cp))
	in.MouseDown(cl.Hex.X+cl.Hex.W/2, cl.Hex.Y+cl.Hex.H/2, 0, false)
	in.MouseUp(cl.Hex.X+cl.Hex.W/2, cl.Hex.Y+cl.Hex.H/2, 0)
	in.key(tree.KeyA, tree.Mods{Ctrl: true})
	for _, r := range "zz!gg" {
		in.Char(r)
	}
	if got := cp.Text.Get(); got != "#ff0000" {
		t.Fatalf("non-hex text changed the value: %q", got)
	}
}

// TestColorPickerTypingFromTheCard: with the picker itself focused, a hex
// character starts editing and replaces the old value; a committed run takes the
// same path.
func TestColorPickerTypingFromTheCard(t *testing.T) {
	_, win, in := inputFixture(t, colorPickerUI, 480, 450)
	cp := MustByID[*ColorPicker](win, "color")
	in.setKbd(cp)
	for _, r := range "ff8800" {
		in.Char(r)
	}
	if !cp.HexActive {
		t.Fatal("typing did not enter the hex field")
	}
	if got := cp.Text.Get(); got != "ff8800" {
		t.Fatalf("typed = %q", got)
	}
	if h, _, _ := cp.HSV(); h < 30 || h > 45 {
		t.Fatalf("hue = %v, want orange", h)
	}

	in.setFocus(nil)
	in.CommitText("#00ff00")
	if got := cp.Text.Get(); got != "#00ff00" {
		t.Fatalf("committed = %q", got)
	}
}

// TestColorPickerAlpha: the alpha bar writes the opacity into the value, and a
// transparent colour keeps its alpha in the hex string.
func TestColorPickerAlpha(t *testing.T) {
	_, win, in := inputFixture(t, colorPickerUI, 480, 470)
	cp := MustByID[*ColorPicker](win, "color")
	cp.Text.Set("#ff000080")
	cp.HSV()
	if cp.Alpha < 0.5 || cp.Alpha > 0.51 {
		t.Fatalf("parsed alpha = %v", cp.Alpha)
	}
	cl := paint.ColorLayoutOf(absoluteRect(cp))
	var last string
	cp.OnEdit(func(v string) { last = v })

	in.MouseDown(cl.Alpha.X+cl.Alpha.W*0.25, cl.Alpha.Y+cl.Alpha.H/2, 0, false)
	in.MouseUp(cl.Alpha.X+cl.Alpha.W*0.25, cl.Alpha.Y+cl.Alpha.H/2, 0)
	if got := cp.Text.Get(); got != "#ff000040" {
		t.Fatalf("alpha pick = %q, want #ff000040", got)
	}
	if last != "#ff000040" {
		t.Fatalf("onEdit = %q", last)
	}
	// Opaque again drops the alpha digits.
	cp.SetHSVA(0, 1, 1, 1)
	if got := cp.Text.Get(); got != "#ff0000" {
		t.Fatalf("opaque = %q", got)
	}
}
