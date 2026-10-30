package paint

import (
	"strings"
	"testing"

	"github.com/ksckaan1/goui/internal/m3"
	"github.com/ksckaan1/goui/internal/tree"
)

// TestShowFPSDrawsCornerLabel checks the debug overlay: off by default, and one
// label in the top-right corner when the app turns it on.
func TestShowFPSDrawsCornerLabel(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Text { text: "hi" } }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	l := list(t, `Window { Text { text: "hi" } }`, 400, 300)
	for _, op := range l.Ops {
		if strings.HasSuffix(op.Text, "fps") {
			t.Fatal("the overlay drew without being asked")
		}
	}
	l = &List{ShowFPS: true, FPS: 57}
	l.Build(tr, 400, 300)
	var found bool
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "57 fps" && op.Align == AlignEnd {
			found = true
		}
	}
	if !found {
		t.Fatalf("no fps label in the top-right:\n%s", l.Dump())
	}
}

// TestTopAppBarWithoutIconDropsTheHamburger keeps the responsive rule honest: an
// empty icon means the navigation lives elsewhere, so no glyph is drawn and the
// title starts at the M3 inset.
func TestTopAppBarWithoutIconDropsTheHamburger(t *testing.T) {
	l := m3list(t, `Window { TopAppBar { label: "Motion" icon: "" } }`)
	var title, menu bool
	var titleX float32
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "Motion" {
			title, titleX = true, op.Rect[0]
		}
		if op.Kind == Text && op.Text == "menu" {
			menu = true
		}
	}
	if !title {
		t.Fatalf("the title is missing:\n%s", l.Dump())
	}
	if menu {
		t.Fatal("an empty icon still drew the hamburger")
	}
	if titleX > 20 {
		t.Fatalf("title starts at %v, want the 16dp inset", titleX)
	}
}

// TestRailSitsOnItsOwnSurface guards the rail being visible: it paints the same
// container tone as the bottom bar, plus a hairline on its right edge, because a
// rail on plain Surface blended into a Surface page.
func TestRailSitsOnItsOwnSurface(t *testing.T) {
	l := m3list(t, `Window {
		NavigationRail { height: 200 NavigationItem { label: "One" icon: tune } }
	}`)
	col := m3.Light().Colors
	var container, edge bool
	for _, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		if op.Color == col.SurfaceContainer && op.Rect[2] > 60 {
			container = true
		}
		if op.Color == col.OutlineVariant && op.Rect[2] <= 1 {
			edge = true
		}
	}
	if !container || !edge {
		t.Fatalf("rail surface: container=%v edge=%v\n%s", container, edge, l.Dump())
	}
}
