package paint

import (
	"testing"

	"github.com/ksckaan1/goui/internal/layout"
	"github.com/ksckaan1/goui/internal/m3"
	"github.com/ksckaan1/goui/internal/tree"
)

// dump builds the list and returns it, failing on a parse error.
func m3list(t *testing.T, src string) *List {
	t.Helper()
	return list(t, src, 400, 400)
}

// TestButtonPaintsFilledContainer checks the M3 button contract: a primary
// container, a state layer when hovered, and a centered label.
func TestButtonPaintsFilledContainer(t *testing.T) {
	l := m3list(t, `Window { Button { label: "Go" } }`)
	col := m3.Light().Colors
	var bg, label bool
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == col.Primary {
			bg = true
		}
		if op.Kind == Text && op.Text == "Go" && op.Color == col.OnPrimary {
			label = true
		}
	}
	if !bg || !label {
		t.Fatalf("button ops missing: bg=%v label=%v\n%s", bg, label, l.Dump())
	}
}

func TestButtonVariants(t *testing.T) {
	for _, tc := range []struct {
		variant string
		want    uint32
	}{
		{"filled", m3.Light().Colors.Primary},
		{"tonal", m3.Light().Colors.SecondaryContainer},
		{"outlined", 0},
	} {
		l := m3list(t, `Window { Button { label: "Go" variant: `+tc.variant+` } }`)
		if len(l.Ops) == 0 || l.Ops[0].Color != tc.want {
			t.Fatalf("%s: first op color = %08x, want %08x", tc.variant, l.Ops[0].Color, tc.want)
		}
	}
}

func TestSliderDrawsTrackAndHandle(t *testing.T) {
	l := m3list(t, `Window { Slider { value: 0.5 min: 0 max: 1 width: 200 } }`)
	col := m3.Light().Colors
	var active, handle bool
	for _, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		if op.Color == col.Primary && op.Rect[2] == 4 {
			handle = true
		}
		if op.Color == col.Primary && op.Rect[2] > 4 {
			active = true
		}
	}
	if !active || !handle {
		t.Fatalf("slider ops missing: active=%v handle=%v\n%s", active, handle, l.Dump())
	}
}

func TestCheckboxDrawsCheckWhenChecked(t *testing.T) {
	l := m3list(t, `Window { Checkbox { checked: true } }`)
	var check, box bool
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "check" {
			check = true
		}
		if op.Kind == Fill && op.Color == m3.Light().Colors.Primary {
			box = true
		}
	}
	if !check || !box {
		t.Fatalf("checked checkbox ops: check=%v box=%v\n%s", check, box, l.Dump())
	}
}

// TestDisabledDimmsEveryPaint is the guard that a disabled control never looks
// enabled: the fill is translucent.
func TestDisabledDimmsEveryPaint(t *testing.T) {
	l := m3list(t, `Window { Button { label: "Go" disabled: true } }`)
	if len(l.Ops) == 0 {
		t.Fatal("no ops")
	}
	if a := l.Ops[0].Color & 0xFF; a > 0x40 {
		t.Fatalf("disabled fill alpha = %d, want low", a)
	}
}

// TestNavigationItemStacksInWideBar guards the arrangement rule: a wide
// destination in a navigation bar still puts the label under the icon.
func TestNavigationItemStacksInWideBar(t *testing.T) {
	l := m3list(t, `Window { NavigationBar { NavigationItem { label: "Home" icon: home } } }`)
	var icon, label Op
	for _, op := range l.Ops {
		if op.Kind != Text {
			continue
		}
		if op.Icon {
			icon = op
		} else {
			label = op
		}
	}
	if icon.Text == "" || label.Text == "" {
		t.Fatalf("missing ops:\n%s", l.Dump())
	}
	if icon.Rect[1] >= label.Rect[1] {
		t.Fatalf("icon y=%v, label y=%v: want icon above label", icon.Rect[1], label.Rect[1])
	}
}

// TestRippleStaysInsideTheComponent guards the press feedback box: the disc is
// wider than a pill while it expands, so clipping it to its ancestors (the
// page) let it spill outside the button.
func TestRippleStaysInsideTheComponent(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			padding: 40
			Button { id: pill width: 200 height: 56 label: "Go" }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("pill")
	r := n.Base().Bounds()
	c := n.(*tree.Button).M3()
	c.RippleOn, c.Ripple, c.Pressed = true, 0.4, true
	c.RippleX, c.RippleY = r.X+r.W/2, r.Y+r.H/2

	l := &List{}
	l.Build(tr, 400, 400)
	var found bool
	for _, op := range l.Ops {
		if op.Shape != Circle || op.ClipRadius == 0 {
			continue
		}
		found = true
		want := [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)}
		if op.Clip != want {
			t.Fatalf("ripple clip = %v, want the button box %v", op.Clip, want)
		}
	}
	if !found {
		t.Fatalf("no clipped ripple op:\n%s", l.Dump())
	}
}

// TestTextFieldSelectionIsHighlighted guards the selection band: it must sit
// behind the value, tinted with the primary color, from the anchor to the caret.
func TestTextFieldSelectionIsHighlighted(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		TextField { id: field label: "Name" width: 240 }
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("field")
	c := n.(*tree.TextField).M3()
	c.Text.Set("hello")
	c.Focused = true
	c.SelAnchor, c.Caret = 1, 4

	l := &List{}
	l.Build(tr, 400, 400)
	want := withAlpha(m3.Light().Colors.Primary, m3.SelectionOpacity)
	for _, op := range l.Ops {
		if op.Shape == RoundedRect && op.Color == want {
			return
		}
	}
	t.Fatalf("no selection band:\n%s", l.Dump())
}
