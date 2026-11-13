package tree

import "testing"

// TestChartSeriesBecomeData covers the declarative path from both sides: the
// children become the chart's data with their style, and the categories string
// becomes the labels. Without it an app had to write every chart from Go.
func TestChartSeriesBecomeData(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		BarChart {
			id:         sales
			categories: ["Jan", "Feb", "Mar"]
			values:     true
			ChartSeries { id: first label: "2026" values: [12, 18, 9] }
			ChartSeries { label: "2027" values: [10, 14, 20] color: tertiary }
			ChartSeries { label: "2028" values: [4] color: #ff0000ff }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, ok := tr.ByID("sales")
	if !ok {
		t.Fatal("no chart")
	}
	chart := n.(*BarChart).ChartCore()
	data := chart.Data
	if len(data.Categories) != 3 || data.Categories[0] != "Jan" || data.Categories[2] != "Mar" {
		t.Fatalf("categories = %v", data.Categories)
	}
	if len(data.Series) != 3 {
		t.Fatalf("series = %+v", data.Series)
	}
	if s := data.Series[0]; s.Label != "2026" || len(s.Values) != 3 || s.Values[1] != 18 {
		t.Fatalf("series 0 = %+v", s)
	}
	// A role stays a role, so the line follows a theme change; a literal is
	// fixed.
	if s := data.Series[1]; s.ColorRole != "tertiary" || s.Color != 0 {
		t.Fatalf("series 1 = %+v", s)
	}
	if s := data.Series[2]; s.Color != 0xFF0000FF || s.ColorRole != "" {
		t.Fatalf("series 2 = %+v", s)
	}
	// The series were authored, not drawn: they are gone from the tree.
	if got := len(chart.Kids()); got != 0 {
		t.Fatalf("the chart kept %d child node(s)", got)
	}
	if _, ok := tr.ByID("first"); ok {
		t.Fatal("a series' id is still registered after it became data")
	}
	// The chart's own properties still work: `values` is the label switch, not
	// the data.
	if !chart.Values.Get() {
		t.Fatal("values: true did not survive next to the series")
	}
}

// TestChartSetDataReplacesDesignData is the rule an app relies on: the file
// gives a chart something to show, and SetData takes over when the data moves.
func TestChartSetDataReplacesDesignData(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		LineChart {
			id: leaf
			ChartSeries { values: [1, 2] }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, _ := tr.ByID("leaf")
	chart := n.(*LineChart).ChartCore()
	chart.SetData(ChartData{Series: []Series{{Values: []float64{7}}}})
	if len(chart.Data.Series) != 1 || chart.Data.Series[0].Values[0] != 7 {
		t.Fatalf("data = %+v", chart.Data)
	}
	if len(chart.Categories.Get()) != 0 {
		t.Fatal("a chart with no categories property still has labels")
	}
}

// TestChartColorsFromDesign covers the two chart colours a design sets and the
// per-slice list: a role stays a role, a literal is fixed.
func TestChartColorsFromDesign(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		PieChart {
			id:         channels
			textColor:  onSurfaceVariant
			gridColor:  #808080ff
			categories: ["Direct", "Search", "Social"]

			ChartSeries {
				values: [42, 27, 18]
				colors: [theme.primary, theme.tertiary, #c62828]
			}
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, _ := tr.ByID("channels")
	chart := n.(*PieChart).ChartCore()
	// A role cell is not resolved at build time: the tree's colour pass writes
	// it from the scheme, which is what keeps a theme change working.
	if role := chart.Base().colorRoleFor("textColor"); role != "onSurfaceVariant" {
		t.Fatalf("textColor role = %q", role)
	}
	if got := chart.GridColor.Get(); got != 0x808080FF {
		t.Fatalf("gridColor = %#08x", got)
	}
	s := chart.Data.Series[0]
	if len(s.Colors) != 3 || s.Colors[2] != 0xC62828FF {
		t.Fatalf("colors = %#v", s.Colors)
	}
	if len(s.ColorRoles) != 3 || s.ColorRoles[0] != "primary" || s.ColorRoles[2] != "" {
		t.Fatalf("color roles = %#v", s.ColorRoles)
	}
}

// TestChartColorRoleFollowsTheScheme is what makes `textColor: onSurface` worth
// writing: the role is resolved by the tree's colour pass, so switching the
// theme repaints the labels.
func TestChartColorRoleFollowsTheScheme(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		BarChart { id: bars textColor: primary gridColor: #808080ff }
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, _ := tr.ByID("bars")
	chart := n.(*BarChart).ChartCore()
	role := func(name string) (uint32, bool) {
		if name == "primary" {
			return 0x6750A4FF, true
		}
		return 0, false
	}
	if !tr.ResolveColors(role) {
		t.Fatal("the colour pass changed nothing")
	}
	if got, want := chart.TextColor.Get(), uint32(0x6750A4FF); got != want {
		t.Fatalf("textColor = %#08x, want %#08x", got, want)
	}
	if got := chart.GridColor.Get(); got != 0x808080FF {
		t.Fatalf("a literal gridColor was overwritten: %#08x", got)
	}
}

// TestChartListItemsReadVariables is the point of an expression list: a series
// value or a slice colour may be a bridge variable, and the chart follows it
// without a SetData call.
func TestChartListItemsReadVariables(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		PieChart {
			id:         channels
			categories: ["Direct", "Search"]
			ChartSeries {
				values: [bridge.a, 2]
				colors: [theme.primary, bridge.ink]
			}
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, _ := tr.ByID("channels")
	chart := n.(*PieChart).ChartCore()
	tr.SetData("a", 5)
	tr.SetData("ink", uint32(0x00FF00FF))
	tr.ApplyExpressions()
	got := chart.Data.Series
	if len(got) != 1 || len(got[0].Values) != 2 || got[0].Values[0] != 5 || got[0].Values[1] != 2 {
		t.Fatalf("values = %+v", got)
	}
	if len(got[0].Colors) != 2 || got[0].Colors[1] != 0x00FF00FF {
		t.Fatalf("colors = %#+v", got[0].Colors)
	}
	if len(got[0].ColorRoles) != 2 || got[0].ColorRoles[0] != "primary" {
		t.Fatalf("roles = %v", got[0].ColorRoles)
	}
	// A second read follows the variable, and an unchanged one costs nothing.
	tr.SetData("a", 9)
	tr.ApplyExpressions()
	if chart.Data.Series[0].Values[0] != 9 {
		t.Fatalf("values after the second set = %v", chart.Data.Series[0].Values)
	}
}

// TestChartCustomColourInAList guards the one role the painter cannot resolve:
// the colour pass writes it into the series, so a list keeps following
// SetCustomColor.
func TestChartCustomColourInAList(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		PieChart {
			id:         channels
			categories: ["a", "b"]
			ChartSeries {
				values: [1, 1]
				colors: [theme.primary, customColor.brand]
			}
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, _ := tr.ByID("channels")
	chart := n.(*PieChart).ChartCore()
	tr.ResolveColors(func(name string) (uint32, bool) {
		if name == "custom:brand" {
			return 0x123456FF, true
		}
		return 0, false
	})
	if got := chart.Data.Series[0].Colors[1]; got != 0x123456FF {
		t.Fatalf("custom colour = %#08x", got)
	}
	if role := chart.Data.Series[0].ColorRoles[1]; role != "custom:brand" {
		t.Fatalf("role = %q", role)
	}
}
