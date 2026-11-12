package tree

import "strconv"

// Series is one line, one group of bars, or the slices of one pie ring.
type Series struct {
	Label string
	// Values is one number per category. A pie reads the first series only.
	Values []float64
	// Color paints the whole series. Colors overrides it per value, which is
	// how a pie colours each slice.
	Color  uint32
	Colors []uint32
}

// ChartData is what a chart draws: the category labels along the value axis and
// one series per line, bar group or ring.
//
// The design file cannot hold a number list, so data is set from Go:
//
//	chart := ui.MustByID[*ui.BarChart](w, "sales")
//	chart.SetData(ui.ChartData{
//		Categories: []string{"Jan", "Feb", "Mar"},
//		Series: []ui.Series{{Label: "2026", Values: []float64{12, 18, 9}}},
//	})
type ChartData struct {
	Categories []string
	Series     []Series
}

// Chart is the state every chart shares: its data, the properties that shape
// it, and the point the pointer is over. A concrete kind embeds it and declares
// only the properties that kind draws.
type Chart struct {
	Component

	Data ChartData

	// Appearance.
	Grid   Prop[bool] // value grid lines
	Axis   Prop[bool] // value and category labels
	Legend Prop[bool] // a key for the series
	Values Prop[bool] // the value written next to each point, bar or slice

	// Shape.
	Stacked     Prop[bool]    // area and bar: add the series instead of grouping
	Horizontal  Prop[bool]    // bar: run the bars left to right
	Smooth      Prop[bool]    // line and area: curve between the points
	Points      Prop[bool]    // line and scatter: a dot at each value
	InnerRadius Prop[float64] // pie: 0 is a pie, above it a donut
	StartAngle  Prop[float64] // pie: degrees, 0 at 12 o'clock
	Min         Prop[float64] // 0 means "from the data"
	Max         Prop[float64]

	// HoverSeries and HoverIndex are the point under the pointer, or -1. The
	// input layer writes them and paint reads them.
	HoverSeries, HoverIndex int
	// SelectedSeries and SelectedIndex are the last clicked point.
	SelectedSeries, SelectedIndex int
	// Hidden turns a series off from its legend entry. A pie hides a slice,
	// every other kind a series. Paint skips it and the value scale follows
	// what is left.
	Hidden []bool
	// LegendHits is the clickable box of each legend entry, in window
	// coordinates. Paint writes it and input reads it, the same way a text
	// field's scroll is shared.
	LegendHits []Rect
}

// ChartCore exposes the shared state to paint, which cannot type-assert five
// concrete kinds.
func (c *Chart) ChartCore() *Chart { return c }

// SetData replaces the chart's data. It is safe to call from Bind.
func (c *Chart) SetData(d ChartData) {
	c.Data = d
	c.HoverSeries, c.HoverIndex = -1, -1
	c.Hidden = nil
	c.Base().MarkDirty(DirtyLayout)
}

// SeriesHidden reports whether the legend turned an entry off.
func (c *Chart) SeriesHidden(i int) bool {
	return i >= 0 && i < len(c.Hidden) && c.Hidden[i]
}

// ToggleHidden flips one entry, resizing the slice the first time. A chart
// whose every entry is hidden keeps the last one on, so a click can never
// leave an empty plot with no way back.
func (c *Chart) ToggleHidden(i int) {
	n := len(c.Data.Series)
	if c.Node().Kind() == "PieChart" {
		n = len(c.Data.Categories)
	}
	if i < 0 || i >= n {
		return
	}
	for len(c.Hidden) < n {
		c.Hidden = append(c.Hidden, false)
	}
	c.Hidden[i] = !c.Hidden[i]
	shown := 0
	for _, h := range c.Hidden[:n] {
		if !h {
			shown++
		}
	}
	if shown == 0 {
		c.Hidden[i] = false
	}
	c.HoverSeries, c.HoverIndex = -1, -1
	c.Base().MarkDirty(DirtyLayout)
}

// CategoryCount is how many points the widest series has.
func (c *Chart) CategoryCount() int {
	n := len(c.Data.Categories)
	for _, s := range c.Data.Series {
		if len(s.Values) > n {
			n = len(s.Values)
		}
	}
	return n
}

// CategoryLabel returns the label for one point, falling back to its index so a
// short Categories list still reads.
func (c *Chart) CategoryLabel(i int) string {
	if i >= 0 && i < len(c.Data.Categories) && c.Data.Categories[i] != "" {
		return c.Data.Categories[i]
	}
	return strconv.Itoa(i + 1)
}

// DataAt returns the value at one point and whether it exists.
func (c *Chart) DataAt(series, i int) (float64, bool) {
	if series < 0 || series >= len(c.Data.Series) {
		return 0, false
	}
	v := c.Data.Series[series].Values
	if i < 0 || i >= len(v) {
		return 0, false
	}
	return v[i], true
}

// chartProps builds the property table of a chart kind: the shared cells this
// kind draws, plus the common ones every element has.
func chartProps(c *Chart, kind string, own ...NamedProp) []NamedProp {
	return append(c.propsExcept(kind,
		"grid", "axis", "legend", "values",
		"stacked", "horizontal", "smooth", "points",
		"innerRadius", "startAngle", "min", "max",
	), own...)
}

// gridProps is the run of properties every chart kind shares.
func gridProps(c *Chart) []NamedProp {
	return []NamedProp{
		BoolProp(&c.NodeBase, "grid", DirtyPaint, &c.Grid),
		BoolProp(&c.NodeBase, "axis", DirtyPaint, &c.Axis),
		BoolProp(&c.NodeBase, "legend", DirtyLayout, &c.Legend),
		BoolProp(&c.NodeBase, "values", DirtyPaint, &c.Values),
		NumberProp(&c.NodeBase, "min", DirtyPaint, &c.Min),
		NumberProp(&c.NodeBase, "max", DirtyPaint, &c.Max),
	}
}

// LineChart draws one line per series through the points.
type LineChart struct{ Chart }

func (*LineChart) Kind() string     { return "LineChart" }
func (c *LineChart) M3() *Component { return &c.Component }
func (c *LineChart) namedProps() []NamedProp {
	return chartProps(&c.Chart, "LineChart",
		append(gridProps(&c.Chart),
			BoolProp(&c.NodeBase, "smooth", DirtyPaint, &c.Smooth),
			BoolProp(&c.NodeBase, "points", DirtyPaint, &c.Points),
		)...)
}

// AreaChart fills the space under each line.
type AreaChart struct{ Chart }

func (*AreaChart) Kind() string     { return "AreaChart" }
func (c *AreaChart) M3() *Component { return &c.Component }
func (c *AreaChart) namedProps() []NamedProp {
	return chartProps(&c.Chart, "AreaChart",
		append(gridProps(&c.Chart),
			BoolProp(&c.NodeBase, "smooth", DirtyPaint, &c.Smooth),
			BoolProp(&c.NodeBase, "points", DirtyPaint, &c.Points),
			BoolProp(&c.NodeBase, "stacked", DirtyPaint, &c.Stacked),
		)...)
}

// BarChart draws one bar per point, grouped or stacked by series.
type BarChart struct{ Chart }

func (*BarChart) Kind() string     { return "BarChart" }
func (c *BarChart) M3() *Component { return &c.Component }
func (c *BarChart) namedProps() []NamedProp {
	return chartProps(&c.Chart, "BarChart",
		append(gridProps(&c.Chart),
			BoolProp(&c.NodeBase, "stacked", DirtyPaint, &c.Stacked),
			BoolProp(&c.NodeBase, "horizontal", DirtyPaint, &c.Horizontal),
		)...)
}

// PieChart draws one slice per value of the first series.
type PieChart struct{ Chart }

func (*PieChart) Kind() string     { return "PieChart" }
func (c *PieChart) M3() *Component { return &c.Component }
func (c *PieChart) namedProps() []NamedProp {
	return chartProps(&c.Chart, "PieChart",
		append(gridProps(&c.Chart),
			NumberProp(&c.NodeBase, "innerRadius", DirtyPaint, &c.InnerRadius),
			NumberProp(&c.NodeBase, "startAngle", DirtyPaint, &c.StartAngle),
		)...)
}

// ScatterChart draws one dot per value, with no line between them.
type ScatterChart struct{ Chart }

func (*ScatterChart) Kind() string     { return "ScatterChart" }
func (c *ScatterChart) M3() *Component { return &c.Component }
func (c *ScatterChart) namedProps() []NamedProp {
	return chartProps(&c.Chart, "ScatterChart",
		append(gridProps(&c.Chart),
			BoolProp(&c.NodeBase, "points", DirtyPaint, &c.Points),
		)...)
}

// RadarChart draws every series as a polygon over one spoke per category.
type RadarChart struct{ Chart }

func (*RadarChart) Kind() string     { return "RadarChart" }
func (c *RadarChart) M3() *Component { return &c.Component }
func (c *RadarChart) namedProps() []NamedProp {
	return chartProps(&c.Chart, "RadarChart",
		append(gridProps(&c.Chart),
			BoolProp(&c.NodeBase, "points", DirtyPaint, &c.Points),
		)...)
}

// GaugeChart shows one value as an arc against its range, with the number in
// the middle.
type GaugeChart struct{ Chart }

func (*GaugeChart) Kind() string     { return "GaugeChart" }
func (c *GaugeChart) M3() *Component { return &c.Component }
func (c *GaugeChart) namedProps() []NamedProp {
	// A gauge has no axes or legend to speak of: one arc, one number.
	return chartProps(&c.Chart, "GaugeChart",
		NumberProp(&c.NodeBase, "min", DirtyPaint, &c.Min),
		NumberProp(&c.NodeBase, "max", DirtyPaint, &c.Max),
		NumberProp(&c.NodeBase, "startAngle", DirtyPaint, &c.StartAngle),
	)
}

// newChart is the zero value of a chart: the props a design may write, and the
// runtime point set to "none".
func newChart() Chart {
	return Chart{
		Component:      newComponent(),
		Grid:           newProp(nil, "grid", DirtyPaint, true),
		Axis:           newProp(nil, "axis", DirtyPaint, true),
		Legend:         newProp(nil, "legend", DirtyLayout, false),
		Values:         newProp(nil, "values", DirtyPaint, false),
		Stacked:        newProp(nil, "stacked", DirtyPaint, false),
		Horizontal:     newProp(nil, "horizontal", DirtyPaint, false),
		Smooth:         newProp(nil, "smooth", DirtyPaint, false),
		Points:         newProp(nil, "points", DirtyPaint, true),
		InnerRadius:    newProp(nil, "innerRadius", DirtyPaint, 0.0),
		StartAngle:     newProp(nil, "startAngle", DirtyPaint, 0.0),
		Min:            newProp(nil, "min", DirtyPaint, 0.0),
		Max:            newProp(nil, "max", DirtyPaint, 0.0),
		HoverSeries:    -1,
		HoverIndex:     -1,
		SelectedSeries: -1,
		SelectedIndex:  -1,
	}
}
