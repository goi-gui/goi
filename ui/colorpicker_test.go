package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

const colorPickerUI = `Window {
	Column {
		padding: 10
		ColorPicker { id: color width: 328 height: 300 text: "#ff0000" }
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
	in.key(tree.KeyEnter, noMods)
	if submits != 1 {
		t.Fatalf("Enter submits = %d", submits)
	}
}
