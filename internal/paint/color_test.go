package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestColorPickerPaintsTheControls: the card has the header, the hue square
// with its two fades, the hue spectrum, the markers and the preview.
func TestColorPickerPaintsTheControls(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		ColorPicker { text: "#ff0000" }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 480, 900)
	l := &List{}
	l.Build(tr, 480, 900)

	var square, hue, preview, alphaBar bool
	var blackFade, whiteFade bool
	var fades int
	texts := map[string]int{}
	for _, op := range l.Ops {
		switch {
		case op.Kind == Fill && op.Shape == CSSGradient:
			if op.Gradient != nil && len(op.Gradient.Stops) >= 2 && op.Gradient.Stops[0].Color&0xFF == 0 {
				alphaBar = true
			} else {
				hue = true
			}
		case op.Kind == Fill && op.Shape == Gradient:
			fades++
			switch op.Color {
			case 0x000000FF:
				blackFade = true
			case 0xFFFFFFFF:
				whiteFade = true
			}
		case op.Kind == Text && !op.Icon:
			texts[op.Text]++
		}
		if op.Kind == Fill && op.Shape == RoundedRect && op.Color == 0xFF0000FF {
			if op.Rect[3] == 40 && op.Radius[0] == 20 {
				preview = true
			}
			if op.Radius[0] == 12 && op.Rect[2] > 200 {
				square = true
			}
		}
	}
	if texts["#ff0000"] != 2 {
		t.Fatalf("hex appears %d times, want the header and the field", texts["#ff0000"])
	}
	if texts["Cancel"] != 1 || texts["OK"] != 1 {
		t.Fatalf("actions = Cancel %d OK %d", texts["Cancel"], texts["OK"])
	}
	if !square {
		t.Fatal("no hue square")
	}
	if fades != 2 {
		t.Fatalf("square fades = %d, want saturation and value", fades)
	}
	if !whiteFade || !blackFade {
		t.Fatalf("fades white=%v black=%v, want both", whiteFade, blackFade)
	}
	if !hue {
		t.Fatal("no hue spectrum")
	}
	if !alphaBar {
		t.Fatal("no alpha fade")
	}
	if !preview {
		t.Fatal("no round preview of the value")
	}
}

// TestColorLayoutHitMapping: the square maps to saturation and value, the bar
// to hue, and both clamp outside their box.
func TestColorLayoutHitMapping(t *testing.T) {
	cl := ColorLayoutOf(tree.Rect{X: 100, Y: 100, W: 328, H: 392})
	s, v := cl.SVAt(cl.SV.X, cl.SV.Y)
	if s != 0 || v != 1 {
		t.Fatalf("top-left = %v %v, want 0 1", s, v)
	}
	s, v = cl.SVAt(cl.SV.Right(), cl.SV.Bottom())
	if s != 1 || v != 0 {
		t.Fatalf("bottom-right = %v %v, want 1 0", s, v)
	}
	s, v = cl.SVAt(cl.SV.X-50, cl.SV.Bottom()+50)
	if s != 0 || v != 0 {
		t.Fatalf("outside = %v %v, want 0 0", s, v)
	}
	if h := cl.HueAt(cl.Hue.X + cl.Hue.W/2); h != 180 {
		t.Fatalf("middle hue = %v, want 180", h)
	}
	if h := cl.HueAt(cl.Hue.Right() + 50); h != 360 {
		t.Fatalf("clamped hue = %v, want 360", h)
	}
	if a := cl.AlphaAt(cl.Alpha.X); a != 0 {
		t.Fatalf("left alpha = %v, want 0", a)
	}
	if a := cl.AlphaAt(cl.Alpha.X + cl.Alpha.W/2); a != 0.5 {
		t.Fatalf("middle alpha = %v, want 0.5", a)
	}
	if a := cl.AlphaAt(cl.Alpha.Right() + 50); a != 1 {
		t.Fatalf("clamped alpha = %v, want 1", a)
	}
}
