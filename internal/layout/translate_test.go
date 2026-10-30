package layout

import "testing"

// TestPositionedTranslateShiftsTheBox covers the enter/exit animation offset:
// a negative translate moves the box off screen while left keeps anchoring it.
func TestPositionedTranslateShiftsTheBox(t *testing.T) {
	tr := build(t, `Window {
		Stack {
			Positioned { id: sheet left: 0 top: 0 bottom: 0 width: 200 translateX: -200 }
		}
	}`)
	New(nil).Layout(tr.Root, 400, 400)
	if got := bounds(t, tr, "sheet"); got.X != -200 || got.W != 200 {
		t.Fatalf("sheet = %+v, want x=-200 w=200", got)
	}
}
