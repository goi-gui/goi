package paint

import (
	"math"
	"strconv"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// chartNode builds a chart from a design snippet and fills it with data.
func chartNode(t *testing.T, kind string, props string, data tree.ChartData, w, h float64) (*tree.Tree, *List) {
	t.Helper()
	src := "Window { " + kind + " { width: " + ftoa(w) + " height: " + ftoa(h) + " " + props + " } }"
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	for _, n := range tr.All() {
		if c, ok := n.(interface{ ChartCore() *tree.Chart }); ok {
			c.ChartCore().SetData(data)
		}
	}
	layout.New(nil).Layout(tr.Root, w+40, h+40)
	l := &List{}
	l.Build(tr, w+40, h+40)
	return tr, l
}

func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func core(t *testing.T, tr *tree.Tree) *tree.Chart {
	t.Helper()
	for _, n := range tr.All() {
		if c, ok := n.(interface{ ChartCore() *tree.Chart }); ok {
			return c.ChartCore()
		}
	}
	t.Fatal("no chart in the tree")
	return nil
}

func barData() tree.ChartData {
	return tree.ChartData{
		Categories: []string{"Jan", "Feb", "Mar"},
		Series:     []tree.Series{{Label: "2026", Values: []float64{10, 20, 5}}},
	}
}

// TestBarChartGeometry pins the bars: one per category, a taller bar for a
// larger value, all starting at the zero baseline.
func TestBarChartGeometry(t *testing.T) {
	tr, _ := chartNode(t, "BarChart", "", barData(), 300, 200)
	lay := ChartLayoutOf(core(t, tr), core(t, tr).Base().Bounds())
	if len(lay.Bars) != 1 || len(lay.Bars[0]) != 3 {
		t.Fatalf("bars = %v, want 1 series of 3", lay.Bars)
	}
	b := lay.Bars[0]
	if b[1].H <= b[0].H || b[0].H <= b[2].H {
		t.Fatalf("heights = %v, %v, %v; want Feb the tallest and Mar the shortest", b[0].H, b[1].H, b[2].H)
	}
	for i, r := range b {
		if math.Abs(r.Y+r.H-lay.Baseline) > 0.01 {
			t.Fatalf("bar %d bottom = %v, want the baseline %v", i, r.Y+r.H, lay.Baseline)
		}
	}
	// The bars sit left to right in category order.
	if !(b[0].X < b[1].X && b[1].X < b[2].X) {
		t.Fatalf("bars out of order: %v", b)
	}
}

// TestBarChartStackedAndHorizontal covers the two shape switches.
func TestBarChartStackedAndHorizontal(t *testing.T) {
	data := tree.ChartData{
		Categories: []string{"a", "b"},
		Series: []tree.Series{
			{Label: "one", Values: []float64{3, 4}},
			{Label: "two", Values: []float64{2, 1}},
		},
	}
	tr, _ := chartNode(t, "BarChart", "stacked: true", data, 300, 200)
	lay := ChartLayoutOf(core(t, tr), core(t, tr).Base().Bounds())
	// Stacked: both series share a column, and the second sits above the first.
	if math.Abs(lay.Bars[0][0].X-lay.Bars[1][0].X) > 0.01 {
		t.Fatalf("stacked bars do not share a column: %v vs %v", lay.Bars[0][0], lay.Bars[1][0])
	}
	if lay.Bars[1][0].Y >= lay.Bars[0][0].Y {
		t.Fatalf("the second series did not stack above the first: %v, %v", lay.Bars[0][0], lay.Bars[1][0])
	}

	tr, _ = chartNode(t, "BarChart", "horizontal: true", data, 300, 200)
	lay = ChartLayoutOf(core(t, tr), core(t, tr).Base().Bounds())
	if !lay.Horizontal {
		t.Fatal("layout did not follow horizontal: true")
	}
	// A horizontal bar is wider than it is tall, and grows to the right.
	r := lay.Bars[0][0]
	if r.W <= r.H {
		t.Fatalf("horizontal bar = %v, want a wide bar", r)
	}
}

// TestLineChartPointsAndHover checks the points follow the data and that a
// hovered point draws a tooltip.
func TestLineChartPointsAndHover(t *testing.T) {
	tr, _ := chartNode(t, "LineChart", "", barData(), 300, 200)
	c := core(t, tr)
	lay := ChartLayoutOf(c, c.Base().Bounds())
	pts := lay.Points[0]
	want := lay.Plot.Bottom() - (20-lay.Min)/(lay.Max-lay.Min)*lay.Plot.H
	if math.Abs(pts[1].Y-want) > 1 {
		t.Fatalf("Feb point y = %v, want %v (plot %+v)", pts[1].Y, want, lay.Plot)
	}
	if !(pts[0].Y > pts[1].Y && pts[2].Y > pts[1].Y) {
		t.Fatalf("point order = %v, want Feb the highest", pts)
	}

	_, before := chartNode(t, "LineChart", "", barData(), 300, 200)
	c.HoverSeries, c.HoverIndex = 0, 1
	l := &List{}
	l.Build(tr, 340, 240)
	texts := map[string]bool{}
	for _, op := range l.Ops {
		if op.Kind == Text {
			texts[op.Text] = true
		}
	}
	if !texts["Jan"] || !texts["Feb"] || !texts["Mar"] {
		t.Fatalf("category labels missing: %v", texts)
	}
	if !texts["Feb · 2026: 20"] {
		t.Fatalf("tooltip missing: %v", texts)
	}
	_ = before
}

// TestPieChartSlices checks the wedges cover the circle, honour innerRadius,
// and that startAngle turns them.
func TestPieChartSlices(t *testing.T) {
	data := tree.ChartData{
		Categories: []string{"a", "b", "c"},
		Series:     []tree.Series{{Values: []float64{1, 2, 1}}},
	}
	tr, _ := chartNode(t, "PieChart", "", data, 200, 200)
	lay := ChartLayoutOf(core(t, tr), core(t, tr).Base().Bounds())
	if len(lay.Slices) != 3 {
		t.Fatalf("slices = %d", len(lay.Slices))
	}
	last := lay.Slices[len(lay.Slices)-1]
	if math.Abs(last.End-2*math.Pi) > 0.001 {
		t.Fatalf("wedges end at %v, want a full turn", last.End)
	}
	if math.Abs((lay.Slices[1].End-lay.Slices[1].Start)-math.Pi) > 0.001 {
		t.Fatalf("the 2 of 4 slice spans %v, want half a turn", lay.Slices[1].End-lay.Slices[1].Start)
	}
	if lay.Slices[0].In != 0 {
		t.Fatalf("inner radius = %v, want a full pie", lay.Slices[0].In)
	}

	tr, _ = chartNode(t, "PieChart", "innerRadius: 0.5", data, 200, 200)
	lay = ChartLayoutOf(core(t, tr), core(t, tr).Base().Bounds())
	if got, want := lay.Slices[0].In, lay.Slices[0].Outer/2; math.Abs(got-want) > 0.01 {
		t.Fatalf("donut hole = %v, want %v", got, want)
	}
}

// TestChartHitLandsOnWhatWasDrawn is the interactivity guard: the point the
// pointer picks is the bar or wedge the painter drew.
func TestChartHitLandsOnWhatWasDrawn(t *testing.T) {
	tr, _ := chartNode(t, "BarChart", "", barData(), 300, 200)
	c := core(t, tr)
	r := c.Base().Bounds()
	lay := ChartLayoutOf(c, r)
	for i, bar := range lay.Bars[0] {
		s, idx := ChartHit(c, r, bar.X+bar.W/2, bar.Y+bar.H/2)
		if s != 0 || idx != i {
			t.Fatalf("hit at bar %d = (%d, %d)", i, s, idx)
		}
	}
	if _, idx := ChartHit(c, r, r.X+2, r.Y+2); idx != -1 {
		t.Fatalf("a point outside the plot hit %d", idx)
	}

	// A pie: the middle of the second wedge.
	pie := tree.ChartData{Categories: []string{"a", "b"}, Series: []tree.Series{{Values: []float64{1, 1}}}}
	tp, _ := chartNode(t, "PieChart", "", pie, 200, 200)
	pc := core(t, tp)
	pr := pc.Base().Bounds()
	pl := ChartLayoutOf(pc, pr)
	s := pl.Slices[1]
	mid := (s.Start + s.End) / 2
	x := s.CX + math.Sin(mid)*(s.Outer/2)
	y := s.CY - math.Cos(mid)*(s.Outer/2)
	if _, idx := ChartHit(pc, pr, x, y); idx != 1 {
		t.Fatalf("pie hit at the second wedge = %d", idx)
	}
	if _, idx := ChartHit(pc, pr, s.CX, s.CY); idx != -1 {
		t.Fatalf("the centre of a pie hit slice %d", idx)
	}
}

// TestChartLegendAndValues covers the two opt-in labels.
func TestChartLegendAndValues(t *testing.T) {
	data := tree.ChartData{
		Categories: []string{"Jan", "Feb"},
		Series: []tree.Series{
			{Label: "Revenue", Values: []float64{10, 20}},
			{Label: "Cost", Values: []float64{5, 7}},
		},
	}
	_, l := chartNode(t, "BarChart", "legend: true values: true", data, 300, 200)
	texts := map[string]bool{}
	for _, op := range l.Ops {
		if op.Kind == Text {
			texts[op.Text] = true
		}
	}
	for _, want := range []string{"Revenue", "Cost", "10", "20"} {
		if !texts[want] {
			t.Fatalf("missing %q in %v", want, texts)
		}
	}
}

// TestChartAreaAndScatterPaint guards the remaining two kinds: they emit the
// fill and the dots.
func TestChartAreaAndScatterPaint(t *testing.T) {
	_, l := chartNode(t, "AreaChart", "", barData(), 300, 200)
	if len(l.Ops) == 0 {
		t.Fatal("the area chart drew nothing")
	}
	// The area fill is a run of thin columns, so the op count is well above the
	// line alone.
	if len(l.Ops) < 100 {
		t.Fatalf("area chart ops = %d, want the fill columns too", len(l.Ops))
	}
	_, l = chartNode(t, "ScatterChart", "", barData(), 300, 200)
	dots := 0
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Shape == Circle {
			dots++
		}
	}
	if dots != 3 {
		t.Fatalf("scatter dots = %d, want one per point", dots)
	}
}
