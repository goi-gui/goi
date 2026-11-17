package paint

import (
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// TestTabsVariantSetsTheBarHeight: primary tabs stack an icon over the label
// and need the taller bar; secondary keeps the existing 48dp.
func TestTabsVariantSetsTheBarHeight(t *testing.T) {
	for _, tc := range []struct {
		variant string
		h       float64
	}{
		{"secondary", 48},
		{"primary", 64},
	} {
		tr, diag := tree.BuildSource([]byte(`Window {
			Column {
				Tabs {
					id:      t
					variant: `+tc.variant+`
					Tab { label: "One" selected: true }
					Tab { label: "Two" }
				}
			}
		}`), nil)
		if !diag.Empty() {
			t.Fatalf("%s: %v", tc.variant, diag)
		}
		layout.New(nil).Layout(tr.Root, 400, 400)
		n, _ := tr.ByID("t")
		if got := n.Base().Bounds().H; got != tc.h {
			t.Errorf("%s height = %v, want %v", tc.variant, got, tc.h)
		}
	}
}

// TestPrimaryTabIndicatorHugsTheContent: the primary underline is as wide as
// the tab's label, centred in the cell, while the secondary one spans it.
func TestPrimaryTabIndicatorHugsTheContent(t *testing.T) {
	l := m3list(t, `Window {
		Tabs {
			variant: primary
			width:   360
			Tab { label: "Flights" leadingIcon: flight selected: true }
			Tab { label: "Trips" leadingIcon: luggage }
			Tab { label: "Explore" leadingIcon: explore }
		}
	}`)
	primary := m3.Light().Colors.Primary
	var indicators []Op
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Rect[3] == 3 && op.Color == primary {
			indicators = append(indicators, op)
		}
	}
	if len(indicators) != 1 {
		t.Fatalf("%d indicators, want one", len(indicators))
	}
	got := float64(indicators[0].Rect[2])
	if got >= 360.0/3-16 {
		t.Fatalf("primary indicator width = %v, want it narrower than the cell", got)
	}
	if got < 24 {
		t.Fatalf("primary indicator width = %v, want at least the icon", got)
	}
	centre := float64(indicators[0].Rect[0]) + got/2
	if math.Abs(centre-60) > 1 {
		t.Fatalf("primary indicator centre = %v, want the first cell's 60", centre)
	}

	l = m3list(t, `Window {
		Tabs {
			variant: secondary
			width:   360
			Tab { label: "Flights" selected: true }
			Tab { label: "Trips" }
		}
	}`)
	if got := indicatorWidth(t, l, primary); got != 180-16 {
		t.Fatalf("secondary indicator width = %v, want the cell minus its 16dp inset", got)
	}
}

// indicatorWidth returns the width of the single primary indicator in l.
func indicatorWidth(t *testing.T, l *List, primary uint32) float64 {
	t.Helper()
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Rect[3] == 3 && op.Color == primary {
			return float64(op.Rect[2])
		}
	}
	t.Fatal("no indicator")
	return 0
}
