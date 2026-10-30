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

// TestSwitchIconGrowsTheThumb covers icon/iconOn/iconOff: an icon enlarges the
// unchecked thumb so the glyph fits, and iconOn wins over icon when checked.
func TestSwitchIconGrowsTheThumb(t *testing.T) {
	thumb := func(l *List) (w float32, icons []string) {
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Shape == Circle {
				w = op.Rect[2]
			}
			if op.Kind == Text && op.Text != "a" {
				icons = append(icons, op.Text)
			}
		}
		return w, icons
	}
	l := m3list(t, `Window { Switch { label: "a" } }`)
	if w, _ := thumb(l); w != 16 {
		t.Fatalf("plain unchecked thumb = %v, want 16", w)
	}
	l = m3list(t, `Window { Switch { label: "a" iconOn: check iconOff: close } }`)
	w, icons := thumb(l)
	if w != 24 {
		t.Fatalf("icon unchecked thumb = %v, want 24", w)
	}
	if len(icons) != 1 || icons[0] != "close" {
		t.Fatalf("unchecked icons = %v, want [close]", icons)
	}
	l = m3list(t, `Window { Switch { label: "a" checked: true icon: star iconOn: check iconOff: close } }`)
	_, icons = thumb(l)
	if len(icons) != 1 || icons[0] != "check" {
		t.Fatalf("checked icons = %v, want [check]", icons)
	}
	l = m3list(t, `Window { Switch { label: "a" checked: true icon: star } }`)
	_, icons = thumb(l)
	if len(icons) != 1 || icons[0] != "star" {
		t.Fatalf("icon fallback = %v, want [star]", icons)
	}
	l = m3list(t, `Window { Switch { label: "a" } }`)
	if _, icons := thumb(l); len(icons) != 0 {
		t.Fatalf("empty icons drew %v", icons)
	}
}
