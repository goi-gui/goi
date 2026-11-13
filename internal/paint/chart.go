package paint

import (
	"math"
	"strconv"

	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// Chart metrics in dp. The gutters are fixed rather than measured from the
// labels, so paint and hit-testing agree without both holding a text measurer.
const (
	chartPad     = 8.0
	chartAxisL   = 44.0 // room for the value labels on the left
	chartAxisB   = 22.0 // room for the category labels below
	chartCatL    = 64.0 // room for the category labels on a horizontal bar
	chartLegendH = 22.0
	chartTicks   = 4 // intervals between the value labels
	chartDotR    = 3.0
	chartLineW   = 2.0
)

// chartPoint is a position in window space; the chart keeps its own so the
// geometry does not depend on a layout type that may change.
type chartPoint struct{ X, Y float64 }

// ChartLayout is where a chart draws. It is a pure function of the box, the
// data and the properties, so paint and hit-testing cannot drift apart.
type ChartLayout struct {
	Box    tree.Rect // the whole element
	Plot   tree.Rect // the data area, excluding the axis labels
	Legend tree.Rect // the key strip, empty when the legend is off

	Min, Max float64
	// Baseline is the position of value 0: a y for a vertical chart, an x for a
	// horizontal bar.
	Baseline float64
	// Ticks is the value scale, bottom to top.
	Ticks []ChartTick
	// CatPos is the centre of each category along the main axis.
	CatPos []float64
	// Points holds each series' point positions: [series][category].
	Points [][]chartPoint
	// Bars holds each bar: [series][category].
	Bars [][]tree.Rect
	// Slices holds the pie geometry, in data order.
	Slices []ChartSlice
	// Horizontal is a bar chart that runs left to right.
	Horizontal bool

	// Radar is the centre and radius of a radar chart, and Spokes the outer
	// end of each category axis.
	CX, CY, Radius float64
	Spokes         []chartPoint
	// Gauge is the arc of a gauge: its box, sweep and the value it shows.
	Gauge GaugeGeometry
}

// GaugeGeometry is where a GaugeChart draws: a ring arc around a centre, with
// the number inside.
type GaugeGeometry struct {
	CX, CY, Radius, Thickness float64
	Start, End                float64
	Value, Min, Max           float64
}

// ChartTick is one value label and the grid line it sits on.
type ChartTick struct {
	Value float64
	Label string
	Pos   float64
}

// ChartSlice is one wedge: angles in radians, 0 at 12 o'clock and growing
// clockwise, the way the painter's arc reads them.
type ChartSlice struct {
	Series, Index     int
	Start, End        float64
	CX, CY, Outer, In float64
}

// ChartLayoutOf computes the geometry of one chart. A chart with no data still
// gets a box, so a design can lay out before Go hands the values over.
func ChartLayoutOf(c *tree.Chart, r tree.Rect) ChartLayout {
	l := ChartLayout{Box: r, Horizontal: c.Horizontal.Get()}
	if r.W <= 0 || r.H <= 0 {
		return l
	}
	inner := tree.Rect{X: r.X + chartPad, Y: r.Y + chartPad,
		W: maxf(r.W-2*chartPad, 0), H: maxf(r.H-2*chartPad, 0)}
	pie := c.Node().Kind() == "PieChart"
	keys := len(c.Data.Series)
	if pie {
		keys = len(c.Data.Categories)
	}
	if c.Legend.Get() && keys > 1 {
		l.Legend = tree.Rect{X: inner.X, Y: inner.Y, W: inner.W, H: minf(chartLegendH, inner.H/3)}
		inner.Y += l.Legend.H
		inner.H = maxf(inner.H-l.Legend.H, 0)
	}
	switch c.Node().Kind() {
	case "PieChart":
		l.LayoutPie(c, inner)
		return l
	case "GaugeChart":
		l.LayoutGauge(c, inner)
		return l
	case "RadarChart":
		l.LayoutRadar(c, inner)
		return l
	}
	l.Plot = tree.Rect{X: inner.X, Y: inner.Y, W: inner.W, H: inner.H}
	if c.Axis.Get() {
		if l.Horizontal {
			l.Plot.X += chartCatL
			l.Plot.W = maxf(l.Plot.W-chartCatL, 0)
			l.Plot.H = maxf(l.Plot.H-chartAxisB, 0)
		} else {
			l.Plot.X += chartAxisL
			l.Plot.W = maxf(l.Plot.W-chartAxisL, 0)
			l.Plot.H = maxf(l.Plot.H-chartAxisB, 0)
		}
	}
	if l.Plot.W <= 0 || l.Plot.H <= 0 {
		return l
	}

	n := c.CategoryCount()
	l.Min, l.Max, l.Baseline = chartRange(c, tree.Rect{X: l.Plot.X, Y: l.Plot.Y, W: l.Plot.W, H: l.Plot.H})
	l.Ticks, l.Min, l.Max = chartScale(l.Min, l.Max, l.Plot, l.Horizontal)
	l.Baseline = chartPos(l, 0)

	l.CatPos = make([]float64, n)
	for i := 0; i < n; i++ {
		if l.Horizontal {
			l.CatPos[i] = l.Plot.Y + (float64(i)+0.5)*l.Plot.H/float64(n)
		} else {
			l.CatPos[i] = l.Plot.X + (float64(i)+0.5)*l.Plot.W/float64(n)
		}
	}

	kind := c.Node().Kind()
	if kind == "BarChart" {
		l.layoutBars(c, n)
		return l
	}
	l.Points = make([][]chartPoint, len(c.Data.Series))
	stackedArea := c.Stacked.Get() && kind == "AreaChart"
	for s := range c.Data.Series {
		pts := make([]chartPoint, n)
		for i := 0; i < n; i++ {
			v := valueAt(c, s, i, stackedArea)
			pts[i] = chartPoint{X: l.CatPos[i], Y: chartPos(l, v)}
		}
		l.Points[s] = pts
	}
	return l
}

// LayoutPie fills in the wedges for a pie or donut.
func (l *ChartLayout) LayoutPie(c *tree.Chart, inner tree.Rect) {
	l.Plot = inner
	if len(c.Data.Series) == 0 {
		return
	}
	values := c.Data.Series[0].Values
	total := 0.0
	for i, v := range values {
		if v > 0 && !c.SeriesHidden(i) {
			total += v
		}
	}
	cx, cy := inner.X+inner.W/2, inner.Y+inner.H/2
	outer := minf(inner.W, inner.H) / 2
	if outer <= 0 || total <= 0 {
		return
	}
	in := clamp01(c.InnerRadius.Get()) * outer
	angle := c.StartAngle.Get() * math.Pi / 180
	l.Slices = make([]ChartSlice, len(values))
	for i, v := range values {
		frac := 0.0
		if v > 0 && !c.SeriesHidden(i) {
			frac = v / total
		}
		end := angle + frac*2*math.Pi
		l.Slices[i] = ChartSlice{
			Series: 0, Index: i, Start: angle, End: end,
			CX: cx, CY: cy, Outer: outer, In: in,
		}
		angle = end
	}
	l.Min, l.Max = 0, total
}

// LayoutRadar places one spoke per category and each series' point along it.
// The spokes start at 12 o'clock and run clockwise, the way the other charts
// read their angles.
func (l *ChartLayout) LayoutRadar(c *tree.Chart, inner tree.Rect) {
	l.Plot = inner
	n := c.CategoryCount()
	if n == 0 {
		return
	}
	l.CX, l.CY = inner.X+inner.W/2, inner.Y+inner.H/2
	// Leave room for the category labels at the end of each spoke.
	l.Radius = minf(inner.W, inner.H)/2 - 26
	if l.Radius <= 8 {
		return
	}
	l.Spokes = make([]chartPoint, n)
	for i := range l.Spokes {
		a := -math.Pi/2 + 2*math.Pi*float64(i)/float64(n)
		l.Spokes[i] = chartPoint{
			X: l.CX + math.Cos(a)*l.Radius,
			Y: l.CY + math.Sin(a)*l.Radius,
		}
	}
	l.Min, l.Max, l.Baseline = chartRange(c, l.Plot)
	l.Ticks, l.Min, l.Max = chartScale(l.Min, l.Max,
		tree.Rect{X: l.CX - l.Radius, Y: l.CY - l.Radius, W: 2 * l.Radius, H: 2 * l.Radius}, false)
	l.Points = make([][]chartPoint, len(c.Data.Series))
	for s := range c.Data.Series {
		pts := make([]chartPoint, n)
		for i := 0; i < n; i++ {
			v, _ := c.DataAt(s, i)
			frac := 0.0
			if l.Max > l.Min {
				frac = clampf((v-l.Min)/(l.Max-l.Min), 0, 1)
			}
			pts[i] = chartPoint{
				X: l.CX + (l.Spokes[i].X-l.CX)*frac,
				Y: l.CY + (l.Spokes[i].Y-l.CY)*frac,
			}
		}
		l.Points[s] = pts
	}
}

// gaugeSweep is how far a gauge arc runs: 240 degrees, leaving the gap at the
// bottom so the ends read as a scale rather than a ring.
const gaugeSweep = 240.0 * math.Pi / 180

// LayoutGauge places the arc and reads the single value it shows.
func (l *ChartLayout) LayoutGauge(c *tree.Chart, inner tree.Rect) {
	l.Plot = inner
	l.CX, l.CY = inner.X+inner.W/2, inner.Y+inner.H/2
	l.Radius = minf(inner.W, inner.H) / 2
	if l.Radius <= 0 {
		return
	}
	l.Gauge = GaugeGeometry{
		CX: l.CX, CY: l.CY,
		Radius:    l.Radius,
		Thickness: clampf(l.Radius*0.18, 6, 24),
		Start:     c.StartAngle.Get()*math.Pi/180 - gaugeSweep/2,
	}
	l.Gauge.End = l.Gauge.Start + gaugeSweep
	v, _ := c.DataAt(0, 0)
	lo, hi := c.Min.Get(), c.Max.Get()
	if lo == 0 && hi == 0 {
		lo, hi = 0, math.Max(v, 1)
	}
	if hi <= lo {
		hi = lo + 1
	}
	l.Gauge.Value, l.Gauge.Min, l.Gauge.Max = v, lo, hi
	l.Min, l.Max = lo, hi
}

// layoutBars places grouped or stacked bars, vertical or horizontal.
func (l *ChartLayout) layoutBars(c *tree.Chart, n int) {
	layout := *l
	ns := len(c.Data.Series)
	l.Bars = make([][]tree.Rect, ns)
	stacked := c.Stacked.Get()
	group := l.Plot.W / float64(max(n, 1))
	if l.Horizontal {
		group = l.Plot.H / float64(max(n, 1))
	}
	for s := range c.Data.Series {
		l.Bars[s] = make([]tree.Rect, n)
	}
	for i := 0; i < n; i++ {
		pos := l.CatPos[i]
		base := l.Baseline
		for s := 0; s < ns; s++ {
			v, _ := c.DataAt(s, i)
			var r tree.Rect
			if stacked {
				end := chartPos(layout, valueAt(c, s, i, true))
				if l.Horizontal {
					r = tree.Rect{X: minf(base, end), Y: pos - group*0.4,
						W: math.Abs(end - base), H: group * 0.8}
				} else {
					r = tree.Rect{X: pos - group*0.4, Y: minf(base, end),
						W: group * 0.8, H: math.Abs(end - base)}
				}
				base = end
			} else if l.Horizontal {
				w := group * 0.8 / float64(max(ns, 1))
				x := pos - group*0.4 + float64(s)*w
				end := chartPos(layout, v)
				r = tree.Rect{X: minf(l.Baseline, end), Y: x, W: math.Abs(end - l.Baseline), H: w}
			} else {
				w := group * 0.8 / float64(max(ns, 1))
				x := pos - group*0.4 + float64(s)*w
				end := chartPos(layout, v)
				r = tree.Rect{X: x, Y: minf(l.Baseline, end), W: w, H: math.Abs(end - l.Baseline)}
			}
			l.Bars[s][i] = r
		}
	}
}

// valueAt returns one value, or the running sum up to series s in a stacked
// chart.
func valueAt(c *tree.Chart, s, i int, stacked bool) float64 {
	if !stacked {
		v, _ := c.DataAt(s, i)
		return v
	}
	sum := 0.0
	for k := 0; k <= s; k++ {
		if c.SeriesHidden(k) {
			continue
		}
		v, _ := c.DataAt(k, i)
		sum += v
	}
	return sum
}

// chartRange is the lowest and highest value the data needs, widened to include
// zero when the drawing is anchored at the baseline.
func chartRange(c *tree.Chart, plot tree.Rect) (lo, hi, baseline float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	kind := c.Node().Kind()
	stacked := c.Stacked.Get() && (kind == "BarChart" || kind == "AreaChart")
	n := c.CategoryCount()
	for i := 0; i < n; i++ {
		if stacked {
			sum := 0.0
			for s := range c.Data.Series {
				if c.SeriesHidden(s) {
					continue
				}
				v, _ := c.DataAt(s, i)
				sum += v
			}
			lo, hi = math.Min(lo, math.Min(sum, 0)), math.Max(hi, math.Max(sum, 0))
			continue
		}
		for s := range c.Data.Series {
			if c.SeriesHidden(s) {
				continue
			}
			v, _ := c.DataAt(s, i)
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if math.IsInf(lo, 1) {
		lo, hi = 0, 1
	}
	if min := c.Min.Get(); min != 0 {
		lo = min
	}
	if max := c.Max.Get(); max != 0 {
		hi = max
	}
	if c.Min.Get() == 0 && c.Max.Get() == 0 {
		// Bars and areas sit on zero; a line may float above it, but a range
		// with no zero in it would magnify noise, so it keeps zero in view.
		switch kind {
		case "BarChart", "AreaChart":
			lo, hi = math.Min(lo, 0), math.Max(hi, 0)
		}
	}
	if kind == "RadarChart" && c.Min.Get() == 0 {
		// A radar's shape is its distance from the centre, so zero belongs on
		// the scale even when a `max` pins the other end.
		lo = math.Min(lo, 0)
	}
	if hi <= lo {
		hi = lo + 1
	}
	_ = plot
	baseline = 0
	return lo, hi, baseline
}

// chartScale rounds the value range out to whole ticks and returns them with
// the range they span.
func chartScale(lo, hi float64, plot tree.Rect, horizontal bool) ([]ChartTick, float64, float64) {
	step := niceStep((hi - lo) / chartTicks)
	lo = math.Floor(lo/step) * step
	hi = math.Ceil(hi/step) * step
	if hi <= lo {
		hi = lo + step
	}
	var ticks []ChartTick
	for v := lo; v <= hi+step/2; v += step {
		var pos float64
		frac := (v - lo) / (hi - lo)
		if horizontal {
			pos = plot.X + frac*plot.W
		} else {
			pos = plot.Bottom() - frac*plot.H
		}
		ticks = append(ticks, ChartTick{Value: v, Label: chartValueLabel(v), Pos: pos})
	}
	return ticks, lo, hi
}

// niceStep rounds a raw step up to 1, 2 or 5 times a power of ten, which is the
// scale ticks people read.
func niceStep(raw float64) float64 {
	if raw <= 0 || math.IsNaN(raw) || math.IsInf(raw, 0) {
		return 1
	}
	pow := math.Pow(10, math.Floor(math.Log10(raw)))
	switch f := raw / pow; {
	case f <= 1:
		return pow
	case f <= 2:
		return 2 * pow
	case f <= 5:
		return 5 * pow
	}
	return 10 * pow
}

// chartPos maps a value to its position along the value axis.
func chartPos(l ChartLayout, v float64) float64 {
	frac := 0.0
	if l.Max > l.Min {
		frac = (v - l.Min) / (l.Max - l.Min)
	}
	if l.Horizontal {
		return l.Plot.X + frac*l.Plot.W
	}
	return l.Plot.Bottom() - frac*l.Plot.H
}

// chartValueLabel formats a tick: whole numbers stay whole, thousands get a k,
// and anything else keeps two decimals at most.
func chartValueLabel(v float64) string {
	a := math.Abs(v)
	switch {
	case a >= 1_000_000:
		return trimZero(strconv.FormatFloat(v/1_000_000, 'f', 1, 64)) + "M"
	case a >= 1000:
		return trimZero(strconv.FormatFloat(v/1000, 'f', 1, 64)) + "k"
	case v == math.Trunc(v):
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return trimZero(strconv.FormatFloat(v, 'f', 2, 64))
}

// trimZero drops a trailing ".0" a one-decimal format leaves behind.
func trimZero(s string) string {
	for _, suffix := range []string{".0", ".00"} {
		if len(s) > len(suffix) && s[len(s)-len(suffix):] == suffix {
			return s[:len(s)-len(suffix)]
		}
	}
	return s
}

// chartHit finds the point under a position: the slice for a pie, otherwise the
// nearest category and, for bars, the bar that contains the point.
func chartHit(c *tree.Chart, r tree.Rect, x, y float64) (series, index int) {
	l := ChartLayoutOf(c, r)
	if c.Node().Kind() == "PieChart" {
		return pieHit(l, x, y)
	}
	switch c.Node().Kind() {
	case "RadarChart":
		return radarHit(l, c, x, y)
	case "GaugeChart":
		// One value, so any press on the dial picks it.
		if l.Plot.Contains(x, y) {
			return 0, 0
		}
		return -1, -1
	}
	if len(l.CatPos) == 0 {
		return -1, -1
	}
	// The nearest category along the main axis.
	best, bestD := -1, math.Inf(1)
	for i, p := range l.CatPos {
		d := math.Abs(p - x)
		if l.Horizontal {
			d = math.Abs(p - y)
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	if best < 0 {
		return -1, -1
	}
	if c.Node().Kind() == "BarChart" {
		for s := range l.Bars {
			if s < len(l.Bars) && best < len(l.Bars[s]) && contains(l.Bars[s][best], x, y) {
				return s, best
			}
		}
		// Inside the strip but between the bars: keep the category, and let
		// paint highlight the group.
		if l.Plot.Contains(x, y) {
			return 0, best
		}
		return -1, -1
	}
	// A line, area or scatter chart: the series whose point is closest.
	bs, bd := 0, math.Inf(1)
	for s := range l.Points {
		if best >= len(l.Points[s]) || c.SeriesHidden(s) {
			continue
		}
		p := l.Points[s][best]
		d := math.Hypot(p.X-x, p.Y-y)
		if d < bd {
			bs, bd = s, d
		}
	}
	if !l.Plot.Contains(x, y) {
		return -1, -1
	}
	return bs, best
}

// radarHit picks the spoke the pointer is nearest in angle, then the series'
// point closest to it.
func radarHit(l ChartLayout, c *tree.Chart, x, y float64) (series, index int) {
	if len(l.Spokes) == 0 {
		return -1, -1
	}
	best, bestD := -1, math.Inf(1)
	for i, sp := range l.Spokes {
		d := math.Hypot(sp.X-x, sp.Y-y)
		if d < bestD {
			best, bestD = i, d
		}
	}
	if best < 0 || !l.Plot.Contains(x, y) {
		return -1, -1
	}
	bs, bd := 0, math.Inf(1)
	for s := range l.Points {
		if best >= len(l.Points[s]) || c.SeriesHidden(s) {
			continue
		}
		p := l.Points[s][best]
		if d := math.Hypot(p.X-x, p.Y-y); d < bd {
			bs, bd = s, d
		}
	}
	return bs, best
}

// pieHit finds the wedge under a position, respecting the hole of a donut.
func pieHit(l ChartLayout, x, y float64) (series, index int) {
	for i, s := range l.Slices {
		dx, dy := x-s.CX, y-s.CY
		r := math.Hypot(dx, dy)
		// The very centre is where every wedge meets: leave it out so a press
		// there does not pick one arbitrary slice.
		if r < math.Max(s.In, 6) || r > s.Outer {
			continue
		}
		// atan2 measured from 12 o'clock, growing clockwise.
		angle := math.Atan2(dx, -dy)
		if angle < 0 {
			angle += 2 * math.Pi
		}
		start, end := math.Mod(s.Start, 2*math.Pi), math.Mod(s.End, 2*math.Pi)
		if start < 0 {
			start += 2 * math.Pi
		}
		if end < 0 {
			end += 2 * math.Pi
		}
		if end < start { // the slice wraps past 12 o'clock
			end += 2 * math.Pi
			if angle < start {
				angle += 2 * math.Pi
			}
		}
		if angle >= start && angle <= end {
			return s.Series, i
		}
	}
	return -1, -1
}

// ChartHit is chartHit for the input layer, which must land on the same point
// the painter drew.
func ChartHit(c *tree.Chart, r tree.Rect, x, y float64) (series, index int) {
	return chartHit(c, r, x, y)
}

// clampf holds v between lo and hi.
func clampf(v, lo, hi float64) float64 {
	if hi < lo {
		return lo
	}
	return math.Max(lo, math.Min(hi, v))
}

// contains reports whether a point is inside a rect.
func contains(r tree.Rect, x, y float64) bool {
	return x >= r.X && x <= r.Right() && y >= r.Y && y <= r.Bottom()
}

// ---------------------------------------------------------------- painting

// paintChart draws the surface, the axes, the series and the hover state.
func (l *List) paintChart(c *tree.Chart, r tree.Rect, clip tree.Rect) {
	if c.Background() != 0 || c.BorderWidth() > 0 {
		l.paintBox(c.Base(), r, clip)
	}
	area := chartArea(r)
	cl := intersect(clip, area)
	lay := ChartLayoutOf(c, r)
	col := l.Theme.Colors
	axisStyle := l.style("labelSmall")

	switch c.Node().Kind() {
	case "PieChart":
		l.paintPie(c, lay, cl)
		l.paintLegend(c, lay, cl, axisStyle, pieColors(c, *l.Theme))
		l.paintChartTooltip(c, lay, cl, axisStyle)
		return
	case "GaugeChart":
		l.paintGauge(c, lay, cl)
		return
	case "RadarChart":
		l.paintRadar(c, lay, cl, axisStyle)
		l.paintLegend(c, lay, cl, axisStyle, seriesPalette(*l.Theme))
		l.paintChartTooltip(c, lay, cl, axisStyle)
		return
	}

	// Grid and value labels.
	if c.Grid.Get() {
		for _, t := range lay.Ticks {
			if lay.Horizontal {
				l.vLine(t.Pos, lay.Plot.Y, lay.Plot.H, withAlpha(chartGrid(c, col.OutlineVariant), 0.45), cl)
			} else {
				l.hLine(lay.Plot.X, t.Pos, lay.Plot.W, withAlpha(chartGrid(c, col.OutlineVariant), 0.45), cl)
			}
		}
	}
	if c.Axis.Get() {
		for _, t := range lay.Ticks {
			if lay.Horizontal {
				box := tree.Rect{X: t.Pos - 28, Y: lay.Plot.Bottom() + 2, W: 56, H: 16}
				l.textIn(t.Label, axisStyle, chartText(c, col.OnSurfaceVariant), box, AlignCenter, cl)
			} else {
				// Keep the top and bottom labels inside the plot, so the legend
				// above it does not overwrite one.
				y := clampf(t.Pos-8, lay.Plot.Y, lay.Plot.Bottom()-16)
				box := tree.Rect{X: lay.Plot.X - chartAxisL + 2, Y: y, W: chartAxisL - 10, H: 16}
				l.textIn(t.Label, axisStyle, chartText(c, col.OnSurfaceVariant), box, AlignEnd, cl)
			}
		}
		for i, p := range lay.CatPos {
			label := c.CategoryLabel(i)
			if lay.Horizontal {
				box := tree.Rect{X: lay.Plot.X - chartCatL + 4, Y: p - 8, W: chartCatL - 10, H: 16}
				l.textIn(label, axisStyle, chartText(c, col.OnSurfaceVariant), box, AlignEnd, cl)
			} else {
				box := tree.Rect{X: p - 40, Y: lay.Plot.Bottom() + 2, W: 80, H: 16}
				l.textIn(label, axisStyle, chartText(c, col.OnSurfaceVariant), box, AlignCenter, cl)
			}
		}
	}

	pal := seriesPalette(*l.Theme)
	if lay.Horizontal {
		l.hLine(lay.Baseline, lay.Plot.Y, lay.Plot.H, withAlpha(col.Outline, 0.7), cl)
	} else {
		l.vLine(lay.Baseline, lay.Plot.Y, lay.Plot.H, withAlpha(col.Outline, 0.7), cl)
	}

	// Hover band behind the series, so the point the tooltip names reads.
	if c.HoverIndex >= 0 && c.HoverIndex < len(lay.CatPos) {
		band := 0.0
		band = 28
		if lay.Horizontal {
			l.fillRect(tree.Rect{X: lay.Plot.X, Y: lay.CatPos[c.HoverIndex] - band/2,
				W: lay.Plot.W, H: band}, withAlpha(col.OnSurface, 0.05), 0, 0, 0, cl)
		} else {
			l.fillRect(tree.Rect{X: lay.CatPos[c.HoverIndex] - band/2, Y: lay.Plot.Y,
				W: band, H: lay.Plot.H}, withAlpha(col.OnSurface, 0.05), 0, 0, 0, cl)
		}
	}

	switch c.Node().Kind() {
	case "BarChart":
		l.paintBars(c, lay, cl, pal)
	case "LineChart", "ScatterChart":
		l.paintLines(c, lay, cl, pal)
	case "AreaChart":
		l.paintAreas(c, lay, cl, pal)
		l.paintLines(c, lay, cl, pal)
	}
	if c.Values.Get() {
		l.paintChartValues(c, lay, cl, axisStyle, pal)
	}
	l.paintLegend(c, lay, cl, axisStyle, pal)
	l.paintChartTooltip(c, lay, cl, axisStyle)
}

// chartArea is the box a chart paints in: its own frame, since the axes live
// inside it.
func chartArea(r tree.Rect) tree.Rect { return r }

// seriesPalette is the chart colour cycle: the scheme's accents first, so a
// chart follows light and dark with no extra configuration.
func seriesPalette(t m3.Theme) []uint32 {
	c := t.Colors
	return []uint32{c.Primary, c.Tertiary, c.Secondary, c.Error, c.PrimaryContainer, c.TertiaryContainer}
}

// chartText is the colour of every label a chart writes, or the scheme's own
// role when the design named none.
func chartText(c *tree.Chart, fallback uint32) uint32 {
	if v := c.TextColor.Get(); v != 0 {
		return v
	}
	return fallback
}

// chartGrid is the colour of the value lines, a radar's rings and spokes, and a
// gauge's track.
func chartGrid(c *tree.Chart, fallback uint32) uint32 {
	if v := c.GridColor.Get(); v != 0 {
		return v
	}
	return fallback
}

// seriesColor is the colour of one series, or the palette's turn. A role
// written in the design is resolved here, so the series follows the scheme.
func seriesColor(c *tree.Chart, t m3.Theme, pal []uint32, s int) uint32 {
	if s < len(c.Data.Series) {
		if role := c.Data.Series[s].ColorRole; role != "" {
			if col, ok := t.Colors.Role(role); ok {
				return col
			}
		}
		if c.Data.Series[s].Color != 0 {
			return c.Data.Series[s].Color
		}
	}
	return pal[s%len(pal)]
}

// pointColor is the colour of one point. A pie colours each slice from the
// palette; an explicit series colour or a per-slice colour wins.
func pointColor(c *tree.Chart, t m3.Theme, pal []uint32, s, i int) uint32 {
	if s < len(c.Data.Series) {
		ser := c.Data.Series[s]
		if i < len(ser.ColorRoles) && ser.ColorRoles[i] != "" {
			if col, ok := t.Colors.Role(ser.ColorRoles[i]); ok {
				return col
			}
		}
		if i < len(ser.Colors) && ser.Colors[i] != 0 {
			return ser.Colors[i]
		}
	}
	if c.Node().Kind() == "PieChart" {
		if s < len(c.Data.Series) && (c.Data.Series[s].Color != 0 || c.Data.Series[s].ColorRole != "") {
			return seriesColor(c, t, pal, s)
		}
		return pal[i%len(pal)]
	}
	return seriesColor(c, t, pal, s)
}

// pieColors hands the legend the colours the slices were drawn with.
func pieColors(c *tree.Chart, t m3.Theme) []uint32 {
	pal := seriesPalette(t)
	out := make([]uint32, 0, len(c.Data.Categories))
	for i := range c.Data.Categories {
		out = append(out, pointColor(c, t, pal, 0, i))
	}
	return out
}

// paintRadar draws the rings, the spokes, and one polygon per series.
func (l *List) paintRadar(c *tree.Chart, lay ChartLayout, clip tree.Rect, style m3.TextStyle) {
	if len(lay.Spokes) < 3 || lay.Radius <= 0 {
		return
	}
	col := l.Theme.Colors
	if c.Grid.Get() {
		// A ring per tick, drawn as a closed polygon through the spokes: on a
		// radar the scale is a shape, not a line.
		for _, t := range lay.Ticks {
			frac := 0.0
			if lay.Max > lay.Min {
				frac = (t.Value - lay.Min) / (lay.Max - lay.Min)
			}
			if frac <= 0 {
				continue
			}
			ring := make([]chartPoint, len(lay.Spokes))
			for i, sp := range lay.Spokes {
				ring[i] = chartPoint{X: lay.CX + (sp.X-lay.CX)*frac, Y: lay.CY + (sp.Y-lay.CY)*frac}
			}
			for i := range ring {
				l.chartSegment(ring[i], ring[(i+1)%len(ring)], withAlpha(chartGrid(c, col.OutlineVariant), 0.45), 1, clip)
			}
		}
	}
	if c.Axis.Get() {
		for i, sp := range lay.Spokes {
			l.chartSegment(chartPoint{X: lay.CX, Y: lay.CY}, sp, withAlpha(chartGrid(c, col.OutlineVariant), 0.45), 1, clip)
			dx, dy := sp.X-lay.CX, sp.Y-lay.CY
			d := math.Hypot(dx, dy)
			if d == 0 {
				continue
			}
			bx := sp.X + dx/d*22 - 40
			by := sp.Y + dy/d*12 - 8
			box := tree.Rect{X: bx, Y: by, W: 80, H: 16}
			l.textIn(c.CategoryLabel(i), style, chartText(c, col.OnSurfaceVariant), box, AlignCenter, clip)
		}
	}
	pal := seriesPalette(*l.Theme)
	for s, pts := range lay.Points {
		if c.SeriesHidden(s) || len(pts) < 2 {
			continue
		}
		color := seriesColor(c, *l.Theme, pal, s)
		// The wash inside the polygon is what makes two series read apart.
		for i := range pts {
			l.fillTriangle(chartPoint{X: lay.CX, Y: lay.CY}, pts[i], pts[(i+1)%len(pts)],
				withAlpha(color, 0.16), clip)
		}
		for i := range pts {
			l.chartSegment(pts[i], pts[(i+1)%len(pts)], color, chartLineW, clip)
		}
		if c.Points.Get() {
			for i, p := range pts {
				radius := chartDotR
				if c.HoverIndex == i && c.HoverSeries == s {
					radius = chartDotR + 2
				}
				l.chartDot(p, radius, color, clip)
			}
		}
		if c.Values.Get() {
			for i, p := range pts {
				v, _ := c.DataAt(s, i)
				box := tree.Rect{X: p.X - 20, Y: p.Y - 18, W: 40, H: 16}
				l.textIn(chartValueLabel(v), style, chartText(c, col.OnSurfaceVariant), box, AlignCenter, clip)
			}
		}
	}
}

// paintGauge draws the track, the filled part and the number in the middle.
func (l *List) paintGauge(c *tree.Chart, lay ChartLayout, clip tree.Rect) {
	g := lay.Gauge
	if g.Radius <= 0 {
		return
	}
	col := l.Theme.Colors
	box := [4]float32{float32(g.CX - g.Radius), float32(g.CY - g.Radius), float32(2 * g.Radius), float32(2 * g.Radius)}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Arc, Rect: box,
		Color:      withAlpha(chartGrid(c, col.SurfaceVariant), 1),
		Thickness:  float32(g.Thickness),
		StartAngle: float32(g.Start),
		EndAngle:   float32(g.End),
		Clip:       clipArr(clip),
	})
	frac := clampf((g.Value-g.Min)/(g.Max-g.Min), 0, 1)
	color := pointColor(c, *l.Theme, seriesPalette(*l.Theme), 0, 0)
	if frac > 0 {
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: Arc, Rect: box,
			Color:      color,
			Thickness:  float32(g.Thickness),
			StartAngle: float32(g.Start),
			EndAngle:   float32(g.Start + (g.End-g.Start)*frac),
			Clip:       clipArr(clip),
		})
	}
	// The number is the point of a gauge, so it is always written.
	style := l.style("headlineSmall")
	label := chartValueLabel(g.Value)
	tw := textWidth(l, label, style)
	nbox := tree.Rect{X: g.CX - tw/2 - 4, Y: g.CY - style.Size*0.8, W: tw + 8, H: style.Size * 1.6}
	l.textIn(label, style, chartText(c, col.OnSurface), nbox, AlignCenter, clip)
}

// fillTriangle fills a triangle with the same rounded-rect pipeline, by
// drawing a thin column per device column: a radar's wash needs a polygon no
// shape kind offers.
func (l *List) fillTriangle(a, b, c chartPoint, color uint32, clip tree.Rect) {
	minX := math.Min(a.X, math.Min(b.X, c.X))
	maxX := math.Max(a.X, math.Max(b.X, c.X))
	if maxX-minX < 1 {
		return
	}
	edges := [][2]chartPoint{{a, b}, {b, c}, {c, a}}
	for x := minX; x < maxX; x += 1 {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, e := range edges {
			p, q := e[0], e[1]
			if (p.X <= x) == (q.X <= x) {
				continue
			}
			t := (x - p.X) / (q.X - p.X)
			y := p.Y + (q.Y-p.Y)*t
			lo, hi = math.Min(lo, y), math.Max(hi, y)
		}
		if hi <= lo {
			continue
		}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect,
			Rect:  [4]float32{float32(x), float32(lo), 1.2, float32(hi - lo)},
			Color: color, Clip: clipArr(clip),
		})
	}
}

// paintBars draws every bar, the hovered one lighter.
func (l *List) paintBars(c *tree.Chart, lay ChartLayout, clip tree.Rect, pal []uint32) {
	rad := c.Base().Radii()[0]
	for s := range lay.Bars {
		if c.SeriesHidden(s) {
			continue
		}
		color := seriesColor(c, *l.Theme, pal, s)
		for i, r := range lay.Bars[s] {
			if r.W <= 0 || r.H <= 0 {
				continue
			}
			col := color
			if c.HoverIndex == i && (c.HoverSeries == s || c.HoverSeries < 0) {
				col = lighten(color, 0.18)
			}
			radius := math.Min(rad, math.Min(r.W, r.H)/2)
			if c.SelectedIndex == i && (c.SelectedSeries == s || c.SelectedSeries < 0) {
				// A selected bar keeps its fill and gains an outline, so the
				// choice reads without another colour.
				l.fillRect(r, col, radius, l.Theme.Colors.OnSurface, 1.5, clip)
				continue
			}
			l.fillRect(r, col, radius, 0, 0, clip)
		}
	}
}

// paintLines draws the line of each series, its points, and the hovered point.
func (l *List) paintLines(c *tree.Chart, lay ChartLayout, clip tree.Rect, pal []uint32) {
	for s, pts := range lay.Points {
		if c.SeriesHidden(s) {
			continue
		}
		color := seriesColor(c, *l.Theme, pal, s)
		if c.Node().Kind() != "ScatterChart" {
			if c.Smooth.Get() {
				l.paintSmooth(pts, color, clip)
			} else {
				for i := 1; i < len(pts); i++ {
					l.chartSegment(pts[i-1], pts[i], color, chartLineW, clip)
				}
			}
		}
		if c.Points.Get() {
			for i, p := range pts {
				radius := chartDotR
				col := color
				if c.HoverIndex == i && c.HoverSeries == s {
					radius, col = chartDotR+2, lighten(color, 0.2)
				}
				if c.SelectedIndex == i && (c.SelectedSeries == s || c.SelectedSeries < 0) {
					l.chartRing(p, chartDotR+3, l.Theme.Colors.OnSurface, clip)
				}
				l.chartDot(p, radius, col, clip)
			}
		}
	}
	// The hovered point gets a ring so it reads over a dense line.
	if c.HoverSeries >= 0 && c.HoverSeries < len(lay.Points) {
		pts := lay.Points[c.HoverSeries]
		if c.HoverIndex >= 0 && c.HoverIndex < len(pts) {
			l.chartRing(pts[c.HoverIndex], chartDotR+4, l.Theme.Colors.OnSurface, clip)
		}
	}
}

// paintSmooth draws a curve through the points with Catmull-Rom segments, which
// reads as a smooth line without overshooting the data.
func (l *List) paintSmooth(pts []chartPoint, color uint32, clip tree.Rect) {
	if len(pts) < 2 {
		return
	}
	// Two segments per device pixel is past the point where a shorter one
	// changes the drawn curve, so the joins stop showing as a staircase without
	// flooding the display list.
	const steps = 16
	for i := 0; i < len(pts)-1; i++ {
		p0 := pts[max(i-1, 0)]
		p1, p2 := pts[i], pts[i+1]
		p3 := pts[min(i+2, len(pts)-1)]
		prev := p1
		for k := 1; k <= steps; k++ {
			t := float64(k) / steps
			t2, t3 := t*t, t*t*t
			x := 0.5 * ((2 * p1.X) + (-p0.X+p2.X)*t + (2*p0.X-5*p1.X+4*p2.X-p3.X)*t2 + (-p0.X+3*p1.X-3*p2.X+p3.X)*t3)
			y := 0.5 * ((2 * p1.Y) + (-p0.Y+p2.Y)*t + (2*p0.Y-5*p1.Y+4*p2.Y-p3.Y)*t2 + (-p0.Y+3*p1.Y-3*p2.Y+p3.Y)*t3)
			next := chartPoint{X: x, Y: y}
			l.chartSegment(prev, next, color, chartLineW, clip)
			prev = next
		}
	}
}

// paintAreas fills the space under each line with thin columns, which is the
// area a trapezoid would cover at every x.
func (l *List) paintAreas(c *tree.Chart, lay ChartLayout, clip tree.Rect, pal []uint32) {
	for s, pts := range lay.Points {
		if len(pts) < 2 || c.SeriesHidden(s) {
			continue
		}
		color := withAlpha(seriesColor(c, *l.Theme, pal, s), 0.22)
		// A stacked area fills down to the series under it, not to zero, which
		// is what keeps the lower band visible.
		lower := func(i int) float64 {
			if !c.Stacked.Get() || s == 0 {
				return lay.Baseline
			}
			return lay.Points[s-1][i].Y
		}
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			ba, bb := lower(i-1), lower(i)
			steps := int(math.Ceil(math.Abs(b.X - a.X)))
			if steps < 1 {
				steps = 1
			}
			for k := 0; k < steps; k++ {
				t := float64(k) / float64(steps)
				x := a.X + (b.X-a.X)*t
				y := a.Y + (b.Y-a.Y)*t
				base := ba + (bb-ba)*t
				l.Ops = append(l.Ops, Op{
					Kind: Fill, Shape: RoundedRect,
					Rect:  [4]float32{float32(x), float32(math.Min(y, base)), 1.2, float32(math.Abs(base - y))},
					Color: color, Clip: clipArr(clip),
				})
			}
		}
	}
}

// chartSegment draws one line segment as a thin rounded quad turned about its
// start, which the shape shader supports directly.
func (l *List) chartSegment(a, b chartPoint, color uint32, width float64, clip tree.Rect) {
	dx, dy := b.X-a.X, b.Y-a.Y
	length := math.Hypot(dx, dy)
	if length <= 0.01 {
		return
	}
	angle := math.Atan2(dy, dx)
	// The quad reaches half a width past both ends, so the rounded caps of two
	// neighbouring segments overlap by a full width. Without it the anti-aliased
	// caps of a curve add up as a bright seam at every join.
	half := width / 2
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:   [4]float32{float32(a.X - half), float32(a.Y - half), float32(length + width), float32(width)},
		Radius: [4]float32{float32(half), float32(half), float32(half), float32(half)},
		Color:  color, Clip: clipArr(clip),
		Rotate: float32(angle), PivotX: float32(a.X), PivotY: float32(a.Y),
	})
}

// chartDot is a filled circle at a point.
func (l *List) chartDot(p chartPoint, radius float64, color uint32, clip tree.Rect) {
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Circle,
		Rect:  [4]float32{float32(p.X - radius), float32(p.Y - radius), float32(radius * 2), float32(radius * 2)},
		Color: color, Clip: clipArr(clip),
	})
}

// chartRing is a circle outline at a point, drawn as a slightly larger dot in
// the ring colour with the chart's own colour punched through.
func (l *List) chartRing(p chartPoint, radius float64, color uint32, clip tree.Rect) {
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Arc,
		Rect:       [4]float32{float32(p.X - radius), float32(p.Y - radius), float32(radius * 2), float32(radius * 2)},
		Color:      color,
		Thickness:  1.5,
		StartAngle: 0,
		EndAngle:   2 * math.Pi,
		Clip:       clipArr(clip),
	})
}

// paintPie draws the wedges, the hovered one pulled out by a few dp.
func (l *List) paintPie(c *tree.Chart, lay ChartLayout, clip tree.Rect) {
	pal := seriesPalette(*l.Theme)
	for i, s := range lay.Slices {
		outer, cx, cy := s.Outer, s.CX, s.CY
		if c.HoverIndex == i || c.SelectedIndex == i {
			outer, cx, cy = s.Outer+4, s.CX, s.CY
		}
		rect := [4]float32{float32(cx - outer), float32(cy - outer), float32(outer * 2), float32(outer * 2)}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: Arc, Rect: rect,
			Color:      pointColor(c, *l.Theme, pal, s.Series, i),
			Thickness:  float32(outer - s.In),
			StartAngle: float32(s.Start),
			EndAngle:   float32(s.End),
			Clip:       clipArr(clip),
		})
	}
	if c.Values.Get() {
		style := l.style("labelSmall")
		for i, s := range lay.Slices {
			if c.SeriesHidden(i) {
				continue
			}
			v, _ := c.DataAt(s.Series, i)
			mid := (s.Start + s.End) / 2
			rr := (s.Outer + s.In) / 2
			label := chartValueLabel(v)
			x := s.CX + math.Sin(mid)*rr
			y := s.CY - math.Cos(mid)*rr
			box := tree.Rect{X: x - 24, Y: y - 8, W: 48, H: 16}
			l.textIn(label, style, chartText(c, l.Theme.Colors.OnSurface), box, AlignCenter, clip)
		}
	}
}

// paintChartValues writes the value beside each bar or point.
func (l *List) paintChartValues(c *tree.Chart, lay ChartLayout, clip tree.Rect, style m3.TextStyle, pal []uint32) {
	col := chartText(c, l.Theme.Colors.OnSurfaceVariant)
	write := func(v float64, r tree.Rect, horizontal bool) {
		label := chartValueLabel(v)
		box := tree.Rect{X: r.X - 20, Y: r.Y - 16, W: 80, H: 16}
		align := AlignStart
		if horizontal {
			box = tree.Rect{X: r.Right() + 4, Y: r.Y + r.H/2 - 8, W: 60, H: 16}
		}
		l.textIn(label, style, col, box, align, clip)
	}
	if len(lay.Bars) > 0 {
		// One label per category, above the tallest bar in the group, so two
		// series do not print their numbers on top of each other.
		for i := range lay.CatPos {
			if _, ok := c.DataAt(0, i); !ok {
				continue
			}
			var top tree.Rect
			label, at := 0.0, 0
			for s := range lay.Bars {
				if i >= len(lay.Bars[s]) || c.SeriesHidden(s) {
					continue
				}
				r := lay.Bars[s][i]
				if r.H == 0 && r.W == 0 {
					continue
				}
				if top.H == 0 && top.W == 0 || r.Y < top.Y || (lay.Horizontal && r.Right() > top.Right()) {
					top, at = r, s
				}
			}
			if top.H == 0 && top.W == 0 {
				continue
			}
			// A stack reads as its total, a group as the bar that reaches
			// furthest.
			if c.Stacked.Get() {
				label = valueAt(c, len(lay.Bars)-1, i, true)
			} else {
				label, _ = c.DataAt(at, i)
			}
			write(label, top, lay.Horizontal)
		}
		return
	}
	for s, pts := range lay.Points {
		if c.SeriesHidden(s) {
			continue
		}
		for i, p := range pts {
			v, _ := c.DataAt(s, i)
			r := tree.Rect{X: p.X - 8, Y: p.Y - 4, W: 16, H: 8}
			write(v, r, false)
		}
		_ = s
	}
}

// paintLegend draws the series key along the top strip.
func (l *List) paintLegend(c *tree.Chart, lay ChartLayout, clip tree.Rect, style m3.TextStyle, colors []uint32) {
	// Recorded every frame: a chart whose legend was turned off must not keep
	// the clickable boxes of the last frame that had one.
	c.LegendHits = c.LegendHits[:0]
	if lay.Legend.H == 0 {
		return
	}
	col := chartText(c, l.Theme.Colors.OnSurfaceVariant)
	x := lay.Legend.X
	y := lay.Legend.Y + lay.Legend.H/2
	for i := range colors {
		if c.Node().Kind() == "PieChart" {
			if i >= len(c.Data.Categories) {
				break
			}
		} else if i >= len(c.Data.Series) {
			break
		}
		label := c.CategoryLabel(i)
		if c.Node().Kind() != "PieChart" {
			label = c.Data.Series[i].Label
		}
		if x > lay.Legend.Right() {
			break
		}
		// A hidden entry stays in place, dimmed, so the click can be undone.
		dim := c.SeriesHidden(i)
		swatch, text := colors[i], col
		if dim {
			swatch, text = withAlpha(swatch, 0.3), withAlpha(col, 0.45)
		}
		tw := textWidth(l, label, style)
		c.LegendHits = append(c.LegendHits, tree.Rect{X: x, Y: y - 10, W: 14 + tw, H: 20})
		l.fillRect(tree.Rect{X: x, Y: y - 5, W: 10, H: 10}, swatch, 3, 0, 0, clip)
		x += 14
		box := tree.Rect{X: x, Y: y - 8, W: tw + 4, H: 16}
		l.textIn(label, style, text, box, AlignStart, clip)
		x += tw + 18
	}
}

// paintChartTooltip draws the point under the pointer as a small panel: the
// category and the value, or the slice's share.
func (l *List) paintChartTooltip(c *tree.Chart, lay ChartLayout, clip tree.Rect, style m3.TextStyle) {
	if c.HoverIndex < 0 {
		return
	}
	var text string
	var anchor chartPoint
	if c.Node().Kind() == "PieChart" {
		if c.HoverIndex >= len(lay.Slices) {
			return
		}
		s := lay.Slices[c.HoverIndex]
		v, _ := c.DataAt(s.Series, c.HoverIndex)
		text = c.CategoryLabel(c.HoverIndex) + "  " + chartValueLabel(v)
		mid := (s.Start + s.End) / 2
		rr := (s.Outer + s.In) / 2
		anchor = chartPoint{X: s.CX + math.Sin(mid)*rr, Y: s.CY - math.Cos(mid)*rr}
	} else {
		if c.HoverSeries < 0 || c.HoverSeries >= len(lay.Points) {
			return
		}
		pts := lay.Points[c.HoverSeries]
		if c.HoverIndex >= len(pts) {
			return
		}
		anchor = pts[c.HoverIndex]
		if len(lay.Bars) > 0 && c.HoverSeries < len(lay.Bars) && c.HoverIndex < len(lay.Bars[c.HoverSeries]) {
			b := lay.Bars[c.HoverSeries][c.HoverIndex]
			anchor = chartPoint{X: b.X + b.W/2, Y: b.Y}
		}
		text = c.CategoryLabel(c.HoverIndex)
		if s := c.Data.Series; c.HoverSeries < len(s) && s[c.HoverSeries].Label != "" {
			text += " · " + s[c.HoverSeries].Label
		}
		if v, ok := c.DataAt(c.HoverSeries, c.HoverIndex); ok {
			text += ": " + chartValueLabel(v)
		}
	}
	tw := textWidth(l, text, style)
	w, h := tw+16, 26.0
	x, y := anchor.X-w/2, anchor.Y-h-10
	// Keep the panel inside the chart box.
	if x < lay.Box.X+2 {
		x = lay.Box.X + 2
	}
	if x+w > lay.Box.Right()-2 {
		x = lay.Box.Right() - 2 - w
	}
	if y < lay.Box.Y+2 {
		y = anchor.Y + 12
	}
	col := l.Theme.Colors
	l.fillRect(tree.Rect{X: x, Y: y, W: w, H: h}, col.InverseSurface, 6, 0, 0, clip)
	box := tree.Rect{X: x + 8, Y: y, W: w - 16, H: h}
	l.textIn(text, style, col.InverseOnSurface, box, AlignCenter, clip)
}

// lighten mixes a colour towards white, which is how a hovered bar or point
// reads without a second palette.
func lighten(c uint32, amount float64) uint32 {
	r := float64((c >> 24) & 0xFF)
	g := float64((c >> 16) & 0xFF)
	b := float64((c >> 8) & 0xFF)
	a := (c & 0xFF)
	mix := func(v float64) uint32 {
		return uint32(v + (255-v)*amount)
	}
	return mix(r)<<24 | mix(g)<<16 | mix(b)<<8 | a
}
