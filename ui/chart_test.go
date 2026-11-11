package ui

import (
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

// chartFixture builds a chart window and returns the shared chart state.
func chartFixture(t *testing.T, kind string) (*interactor, *tree.Chart) {
	t.Helper()
	_, win, in := inputFixture(t, `Window { `+kind+` {
		id:     c
		width:  400
		height: 240
	} }`, 400, 240)
	var c *tree.Chart
	for _, n := range win.tree.All() {
		if h, ok := n.(interface{ ChartCore() *tree.Chart }); ok {
			c = h.ChartCore()
		}
	}
	if c == nil {
		t.Fatal("no chart in the tree")
	}
	c.SetData(tree.ChartData{
		Categories: []string{"Jan", "Feb", "Mar"},
		Series: []tree.Series{
			{Label: "2026", Values: []float64{10, 40, 20}},
			{Label: "2025", Values: []float64{5, 15, 10}},
		},
	})
	return in, c
}

// TestChartHoverFollowsThePointer is the interactivity guard at the input
// layer: moving over a bar sets the hovered point, moving off clears it, and the
// onHover handler hears about both.
func TestChartHoverFollowsThePointer(t *testing.T) {
	in, c := chartFixture(t, "BarChart")
	var hovered []int
	c.HoverPoint.On(func(i int) { hovered = append(hovered, i) })

	r := absoluteRect(c.Node())
	lay := paint.ChartLayoutOf(c, tree.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H})
	bar := lay.Bars[0][1]
	in.MouseMove(bar.X+bar.W/2, bar.Y+bar.H/2)
	if c.HoverSeries != 0 || c.HoverIndex != 1 {
		t.Fatalf("hover = (%d, %d), want the second bar of the first series", c.HoverSeries, c.HoverIndex)
	}
	if len(hovered) != 1 || hovered[0] != 1 {
		t.Fatalf("onHover calls = %v, want [1]", hovered)
	}

	// Leaving the chart clears the point and reports -1.
	in.MouseMove(r.X+r.W+50, r.Y+r.H+50)
	if c.HoverIndex != -1 {
		t.Fatalf("hover index after leaving = %d, want -1", c.HoverIndex)
	}
	if len(hovered) != 2 || hovered[1] != -1 {
		t.Fatalf("onHover calls = %v, want a trailing -1", hovered)
	}
}

// TestChartClickSelectsAPoint checks that a press picks the bar under it and
// fires onSelect with its index.
func TestChartClickSelectsAPoint(t *testing.T) {
	in, c := chartFixture(t, "BarChart")
	var picked int = -1
	c.OnSelect(func(i int) { picked = i })

	r := absoluteRect(c.Node())
	lay := paint.ChartLayoutOf(c, tree.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H})
	bar := lay.Bars[0][2]
	in.MouseDown(bar.X+bar.W/2, bar.Y+bar.H/2, 0, false)
	in.MouseUp(bar.X+bar.W/2, bar.Y+bar.H/2, 0)

	if picked != 2 {
		t.Fatalf("onSelect index = %d, want 2", picked)
	}
	if c.SelectedSeries != 0 || c.SelectedIndex != 2 {
		t.Fatalf("selection = (%d, %d), want the third bar of the first series", c.SelectedSeries, c.SelectedIndex)
	}
}

// TestChartClickWorksOnAPie checks the wedge hit test through the same press
// path, since a pie has no bars to fall back on.
func TestChartClickWorksOnAPie(t *testing.T) {
	in, c := chartFixture(t, "PieChart")
	var picked int = -1
	c.OnSelect(func(i int) { picked = i })

	r := absoluteRect(c.Node())
	lay := paint.ChartLayoutOf(c, tree.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H})
	s := lay.Slices[2]
	mid := (s.Start + s.End) / 2
	x := s.CX + math.Sin(mid)*(s.Outer*0.6)
	y := s.CY - math.Cos(mid)*(s.Outer*0.6)
	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if picked != 2 {
		t.Fatalf("pie pick = %d, want the third wedge", picked)
	}
}
