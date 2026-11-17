package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// TestSliderTooltipNamesTheHandle covers showTooltip: the bubble is absent at
// rest, follows the hovered handle of a range slider, keeps the grabbed one
// while dragging, and stays away from a disabled slider.
func TestSliderTooltipNamesTheHandle(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			Slider {
				id:          s
				variant:     range
				value:       20
				end:         70
				min:         0
				max:         100
				showTooltip: true
				width:       300
				height:      40
			}
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("s")
	sl := n.(*tree.Slider)
	s := &sl.Component
	paint := func() *List {
		l := &List{}
		l.Build(tr, 400, 400)
		return l
	}
	values := func(l *List) (texts []string, bubble *Op) {
		for i := range l.Ops {
			op := &l.Ops[i]
			if op.Kind == Text {
				texts = append(texts, op.Text)
			}
			if op.Kind == Fill && op.Color == m3.Light().Colors.InverseSurface {
				bubble = op
			}
		}
		return
	}

	if texts, _ := values(paint()); len(texts) != 0 {
		t.Fatalf("a slider at rest painted a tooltip: %v", texts)
	}

	s.Hover = true
	s.Base().MarkDirty(tree.DirtyPaint)
	if texts, _ := values(paint()); len(texts) != 1 || texts[0] != "20" {
		t.Fatalf("hovering the low handle showed %v, want 20", texts)
	}
	sl.HoverGrab = 1
	if texts, _ := values(paint()); len(texts) != 1 || texts[0] != "70" {
		t.Fatalf("hovering the high handle showed %v, want 70", texts)
	}

	// A drag keeps naming the grabbed handle even before the pointer moves.
	sl.HoverGrab = 0
	sl.Pressed = true
	sl.Grab = 1
	if texts, _ := values(paint()); len(texts) != 1 || texts[0] != "70" {
		t.Fatalf("dragging the high handle showed %v, want 70", texts)
	}
	sl.Grab = 0
	if texts, _ := values(paint()); len(texts) != 1 || texts[0] != "20" {
		t.Fatalf("dragging the low handle showed %v, want 20", texts)
	}
	sl.Pressed = false

	// The bubble sits above the track, not over it.
	cy := float64(s.Base().Bounds().Y) + s.Base().Bounds().H/2
	if _, bubble := values(paint()); bubble == nil || float64(bubble.Rect[1]+bubble.Rect[3]) > cy {
		t.Fatalf("bubble = %+v, want it above the track centre %v", bubble, cy)
	}

	s.Disabled.Set(true)
	if texts, _ := values(paint()); len(texts) != 0 {
		t.Fatalf("a disabled slider painted a tooltip: %v", texts)
	}
}

// TestSliderTooltipIsOffByDefault keeps showTooltip opt-in.
func TestSliderTooltipIsOffByDefault(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Slider { id: s value: 0.4 min: 0 max: 1 width: 300 height: 40 }
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("s")
	sl := n.(*tree.Slider)
	s := &sl.Component
	s.Hover = true
	l := &List{}
	l.Build(tr, 400, 400)
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "0.4" {
			t.Fatal("showTooltip is false, but the value was painted")
		}
	}
}
