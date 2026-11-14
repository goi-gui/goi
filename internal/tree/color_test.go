package tree

import "testing"

// TestHSVColor checks the conversion the picker paints with.
func TestHSVColor(t *testing.T) {
	cases := []struct {
		h, s, v float64
		want    uint32
	}{
		{0, 1, 1, 0xFF0000FF},
		{120, 1, 1, 0x00FF00FF},
		{240, 1, 1, 0x0000FFFF},
		{360, 1, 1, 0xFF0000FF}, // 360 is the same turn as 0
		{0, 0, 0, 0x000000FF},
		{0, 0, 1, 0xFFFFFFFF},
	}
	for _, c := range cases {
		if got := HSVColor(c.h, c.s, c.v); got != c.want {
			t.Errorf("HSVColor(%v, %v, %v) = %08x, want %08x", c.h, c.s, c.v, got, c.want)
		}
	}
}

// TestHexHSV reads a CSS colour back into the controls, and formats it again.
func TestHexHSV(t *testing.T) {
	h, s, v, ok := hexHSV("#00ff00")
	if !ok || h != 120 || s != 1 || v != 1 {
		t.Fatalf("hexHSV green = %v %v %v %v", h, s, v, ok)
	}
	if got := hsvHex(h, s, v); got != "#00ff00" {
		t.Fatalf("hsvHex = %q", got)
	}
	// A name parses too; the picker shows its hex spelling.
	h, s, v, ok = hexHSV("red")
	if !ok || h != 0 || s != 1 || v != 1 {
		t.Fatalf("hexHSV red = %v %v %v %v", h, s, v, ok)
	}
	if _, _, _, ok := hexHSV("not a colour"); ok {
		t.Fatal("a bad value parsed")
	}
}

// TestColorPickerFollowsText: an external value re-seeds the HSV once, and the
// hue survives a black or grey value the hex cannot express.
func TestColorPickerFollowsText(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window { ColorPicker { id: pick text: "#0000ff" } }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	var cp *ColorPicker
	tr.Walk(func(n Node) {
		if c, ok := n.(*ColorPicker); ok {
			cp = c
		}
	})
	if cp == nil {
		t.Fatal("no ColorPicker")
	}
	if h, s, v := cp.HSV(); h != 240 || s != 1 || v != 1 {
		t.Fatalf("blue = %v %v %v", h, s, v)
	}
	cp.SetHSV(240, 0, 0)
	if got := cp.Text.Get(); got != "#000000" {
		t.Fatalf("black text = %q", got)
	}
	if h, _, _ := cp.HSV(); h != 240 {
		t.Fatalf("hue after black = %v, want 240", h)
	}
	// A value set from Go is read on the next frame.
	cp.Text.Set("#ff0000")
	if h, s, v := cp.HSV(); h != 0 || s != 1 || v != 1 {
		t.Fatalf("externally set red = %v %v %v", h, s, v)
	}
	if got := cp.Hex(); got != "#ff0000" {
		t.Fatalf("Hex = %q", got)
	}
	// A pick at the far right of the hue bar keeps 360, so the marker stays at
	// the edge; it is still red.
	if got := cp.SetHSV(360, 1, 1); got != "#ff0000" {
		t.Fatalf("360 = %q", got)
	}
	if cp.Hue != 360 {
		t.Fatalf("hue at the right edge = %v, want 360", cp.Hue)
	}
}
