package paint

import (
	"testing"

	"github.com/ksckaan1/goi/internal/layout"
	"github.com/ksckaan1/goi/internal/m3"
	"github.com/ksckaan1/goi/internal/tree"
)

// TestSwitchHandleTravels paints a switch at a few positions along the toggle.
// The ui layer owns the animation; this checks that the painter actually reads
// the position, so a mid-travel frame is not the same picture as a settled one.
func TestSwitchHandleTravels(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Switch { id: sw label: "Wi-Fi" } }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	node, _ := tr.ByID("sw")
	c := node.(interface{ M3() *tree.Component }).M3()
	c.TogglePosed = true

	thumb := func(pos float64) (x, d float32) {
		c.TogglePos = pos
		c.Base().MarkDirty(tree.DirtyPaint)
		layout.New(nil).Layout(tr.Root, 400, 300)
		l := &List{}
		l.Build(tr, 400, 300)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Shape == Circle {
				return op.Rect[0] + op.Rect[2]/2, op.Rect[2]
			}
		}
		return 0, 0
	}

	offX, offD := thumb(0)
	midX, midD := thumb(0.5)
	onX, onD := thumb(1)

	if offD != 16 || onD != 24 {
		t.Fatalf("thumb size %v -> %v, want 16 -> 24", offD, onD)
	}
	if midD <= offD || midD >= onD {
		t.Fatalf("mid-travel thumb = %v, want between %v and %v", midD, offD, onD)
	}
	if !(offX < midX && midX < onX) {
		t.Fatalf("handle %v -> %v -> %v is not a travel", offX, midX, onX)
	}
	// 20dp of travel: the track is 52 wide and the centres are 16 and 36 in.
	if want := (onX - offX) - 20; want > 0.5 || want < -0.5 {
		t.Fatalf("travel = %v, want 20", onX-offX)
	}
}

// TestSwitchMidTravelFadesTheOutline keeps the half way frame honest: the
// unchecked outline is gone only at the on end, so it must be drawn with a
// thinner border in between rather than snapping off.
func TestSwitchMidTravelFadesTheOutline(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Switch { id: sw } }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	node, _ := tr.ByID("sw")
	c := node.(interface{ M3() *tree.Component }).M3()
	c.TogglePosed = true
	c.TogglePos = 0.5
	c.Base().MarkDirty(tree.DirtyPaint)
	layout.New(nil).Layout(tr.Root, 400, 300)
	l := &List{}
	l.Build(tr, 400, 300)

	col := m3.Light().Colors
	for _, op := range l.Ops {
		if op.Kind == Fill && op.BorderColor == col.Outline &&
			op.BorderWidth > 0.5 && op.BorderWidth < 1.5 {
			return
		}
	}
	t.Fatalf("no half-faded outline at mid travel:\n%s", l.Dump())
}
