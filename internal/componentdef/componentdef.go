// Package componentdef is the single list of Material 3 component kinds and
// the properties each one accepts. tree builds its property tables from it and
// schema validates against it, so the two cannot drift apart.
package componentdef

import "math"

// Kind is one component element name.
type Kind struct {
	Name string
	// Container marks components that take child nodes.
	Container bool
}

// Kinds lists every Material 3 component, in the order the docs group them.
var Kinds = []Kind{
	// Layout and structure.
	{"Divider", false},
	// Content: components that are plain nodes with behaviour.
	{"Code", false},
	{"Markdown", false},
	// Actions.
	{"Button", false},
	{"IconButton", false},
	{"FAB", false},
	{"FABMenu", true},
	{"ButtonGroup", true},
	{"SplitButton", false},
	{"SegmentedButton", false},
	{"Chip", false},
	{"Toolbar", true},
	// Selection controls.
	{"Checkbox", false},
	{"RadioButton", false},
	{"Switch", false},
	{"Slider", false},
	// Text input.
	{"TextField", false},
	{"SearchBar", false},
	{"TextArea", false},
	// Containment.
	{"Card", true},
	{"Dialog", true},
	{"BottomSheet", true},
	{"SideSheet", true},
	{"List", true},
	{"ListItem", false},
	{"Menu", true},
	{"MenuItem", false},
	{"Snackbar", false},
	{"Tooltip", true},
	{"Table", true},
	{"TableColumn", false},
	{"TableRow", true},
	{"TableCell", true},
	{"Carousel", true},
	// Navigation.
	{"TopAppBar", false},
	{"NavigationBar", true},
	{"NavigationRail", true},
	{"NavigationDrawer", true},
	{"NavigationItem", false},
	{"Tabs", true},
	{"Tab", false},
	// Status and progress.
	{"Badge", false},
	{"LinearProgress", false},
	{"CircularProgress", false},
	{"LoadingIndicator", false},
	// Date and time.
	{"DatePicker", true},
	{"TimePicker", true},
	// Charts.
	{"LineChart", false},
	{"AreaChart", false},
	{"BarChart", false},
	{"PieChart", false},
	{"ScatterChart", false},
	{"RadarChart", false},
	{"GaugeChart", false},
}

var byName = func() map[string]Kind {
	m := make(map[string]Kind, len(Kinds))
	for _, k := range Kinds {
		m[k.Name] = k
	}
	return m
}()

// Lookup returns the kind definition.
func Lookup(name string) (Kind, bool) {
	k, ok := byName[name]
	return k, ok
}

// props is the property list per kind, beyond the common box properties.
var props = map[string][]string{
	"Button":           {"label", "icon", "variant", "color", "disabled"},
	"IconButton":       {"icon", "variant", "disabled", "selected"},
	"FAB":              {"label", "icon", "variant", "disabled", "expanded"},
	"FABMenu":          {"icon", "variant", "disabled", "expanded"},
	"ButtonGroup":      {"variant", "disabled"},
	"SplitButton":      {"label", "icon", "variant", "disabled", "expanded"},
	"SegmentedButton":  {"label", "icon", "variant", "disabled", "selected"},
	"Chip":             {"label", "icon", "trailingIcon", "variant", "disabled", "selected", "checked"},
	"Badge":            {"label", "variant"},
	"Checkbox":         {"label", "variant", "disabled", "checked"},
	"RadioButton":      {"label", "variant", "disabled", "checked"},
	"Switch":           {"label", "variant", "disabled", "checked", "icon", "iconOn", "iconOff"},
	"Slider":           {"variant", "disabled", "value", "end", "center", "min", "max", "step"},
	"TextField":        {"label", "text", "placeholder", "supporting", "icon", "trailingIcon", "variant", "color", "disabled"},
	"SearchBar":        {"label", "text", "placeholder", "icon", "trailingIcon", "variant", "disabled"},
	"Tooltip":          {"text", "variant"},
	"Card":             {"label", "supporting", "icon", "variant", "disabled"},
	"Dialog":           {"label", "supporting", "icon", "variant"},
	"BottomSheet":      {"label", "supporting", "variant"},
	"SideSheet":        {"label", "supporting", "variant"},
	"Divider":          {"variant"},
	"List":             {"variant"},
	"ListItem":         {"label", "supporting", "icon", "trailingIcon", "variant", "disabled", "selected"},
	"Table":            {"variant", "striped", "showHeader", "stickyHeader", "rowHeight", "headerHeight", "sortColumn", "sortAscending", "selectable", "selectedRow"},
	"TableColumn":      {"label", "icon", "key", "align", "sortable", "numeric"},
	"TableRow":         {"selected", "disabled"},
	"TableCell":        {"value", "icon", "align"},
	"Menu":             {"variant"},
	"MenuItem":         {"label", "icon", "trailingIcon", "variant", "disabled", "selected"},
	"Snackbar":         {"label", "variant"},
	"TopAppBar":        {"label", "variant", "icon", "trailingIcon"},
	"NavigationBar":    {"variant"},
	"NavigationRail":   {"variant"},
	"NavigationDrawer": {"label", "variant"},
	"NavigationItem":   {"label", "icon", "variant", "disabled", "selected"},
	"Tabs":             {"variant"},
	"Tab":              {"label", "icon", "variant", "disabled", "selected"},
	"LinearProgress":   {"variant", "value", "min", "max", "thickness"},
	"CircularProgress": {"variant", "value", "min", "max", "thickness"},
	"LoadingIndicator": {"variant", "color"},
	"DatePicker":       {"label", "text", "variant"},
	"TimePicker":       {"label", "text", "variant"},
	"Carousel":         {"variant"},
	"Toolbar":          {"variant"},
	"TextArea":         {"label", "text", "placeholder", "supporting", "variant", "disabled"},
	"Code":             {"text", "language", "variant", "showCopy"},
	"Markdown":         {"text"},
	"LineChart":        {"grid", "axis", "legend", "categories", "textColor", "gridColor", "values", "smooth", "points", "min", "max"},
	"AreaChart":        {"grid", "axis", "legend", "categories", "textColor", "gridColor", "values", "smooth", "points", "stacked", "min", "max"},
	"BarChart":         {"grid", "axis", "legend", "categories", "textColor", "gridColor", "values", "stacked", "horizontal", "min", "max"},
	"PieChart":         {"grid", "axis", "legend", "categories", "textColor", "gridColor", "values", "innerRadius", "startAngle", "min", "max"},
	"ScatterChart":     {"grid", "axis", "legend", "categories", "textColor", "gridColor", "values", "points", "min", "max"},
	"RadarChart":       {"grid", "axis", "legend", "categories", "textColor", "gridColor", "values", "points", "min", "max"},
	"GaugeChart":       {"textColor", "gridColor", "min", "max", "startAngle"},
}

// Chart reports whether kind draws a chart, which is what tells a zero `max`
// ("follow the data") from a progress bar's 1.
func Chart(kind string) bool {
	switch kind {
	case "LineChart", "AreaChart", "BarChart", "PieChart", "ScatterChart", "RadarChart", "GaugeChart":
		return true
	}
	return false
}

// Flow lists containers that accept the row/column layout properties.
func Flow(kind string) bool {
	switch kind {
	case "ButtonGroup", "Toolbar", "Carousel", "FABMenu", "Tabs",
		"NavigationBar", "NavigationRail", "NavigationDrawer", "List", "Menu",
		"Card", "Dialog", "BottomSheet", "SideSheet", "DatePicker", "TimePicker",
		"Tooltip":
		return true
	}
	return false
}

// Props returns the component-specific property names for a kind.
func Props(kind string) []string {
	base := props[kind]
	if !Flow(kind) {
		return base
	}
	out := make([]string, 0, len(base)+4)
	out = append(out, base...)
	return append(out, "spacing", "gap", "crossAxisAlign", "mainAxisAlign")
}

// PropType is the literal type a component property needs.
type PropType int

// Property types.
const (
	String PropType = iota
	Enum
	Bool
	Number
	// Scalar is a value that may be written as a string, number or bool.
	Scalar
	// Color is a theme role or a CSS colour, which is what a chart's text and
	// grid take.
	Color
	// List is a list literal, `[1, 2, 3]`, whose items ListElem types.
	List
)

// defaults are the component properties whose initial value is not the Go zero
// value. The schema applies them, so the builder and the validator agree on
// what a property means before the design writes it.
var defaults = map[string]any{
	// Charts draw their chrome by default, except the legend and the value
	// labels, which most panels leave off.
	"grid":          true,
	"axis":          true,
	"points":        true,
	"legend":        false,
	"values":        false,
	"showHeader":    true,
	"rowHeight":     48.0,
	"headerHeight":  40.0,
	"sortColumn":    -1.0,
	"sortAscending": true,
	"selectable":    "none",
	// A code block offers its copy button until the design says otherwise.
	"showCopy":    true,
	"selectedRow": -1.0,
	// A centered slider fills from the midpoint of min and max until the
	// design names a center of its own.
	"center": math.NaN(),
}

// Default returns the initial value of a component property, when it is not the
// zero value for its type.
func Default(name string) (any, bool) {
	v, ok := defaults[name]
	return v, ok
}

// TypeOfFor is TypeOf narrowed per component where one name means two types:
// a `value` is a number on a Slider and the sort string on a TableCell.
func TypeOfFor(kind, name string) PropType {
	if kind == "TableCell" && name == "value" {
		return Scalar
	}
	return TypeOf(name)
}

// TypeOf returns the literal type of a component property.
func TypeOf(name string) PropType {
	switch name {
	case "disabled", "checked", "selected", "expanded", "showCopy",
		"sortable", "numeric", "striped", "showHeader", "stickyHeader", "sortAscending",
		"grid", "axis", "legend", "values", "stacked", "horizontal", "smooth", "points":
		return Bool
	case "value", "end", "center", "min", "max", "step", "spacing", "gap", "size",
		"rowHeight", "headerHeight", "sortColumn", "selectedRow",
		"innerRadius", "startAngle", "thickness":
		return Number
	case "variant", "crossAxisAlign", "mainAxisAlign", "align", "selectable":
		return Enum
	case "textColor", "gridColor", "color":
		return Color
	case "categories":
		return List
	}
	return String
}

// ListElem is the type of one item of a list property, which is what the
// checker reads each entry as. Only a chart's categories are a list today, and
// a label is a string.
func ListElem(name string) PropType { return String }

// variantValues narrows the shared variant union where a component paints only
// a few styles, so completion and the checker do not offer a word the painter
// would ignore.
var variantValues = map[string][]string{
	"Table":            {"outlined", "filled", "plain"},
	"Slider":           {"standard", "centered", "range"},
	"LinearProgress":   {"normal", "wavy"},
	"CircularProgress": {"normal", "wavy"},
}

// EnumValuesFor is EnumValues narrowed to the values one component implements.
func EnumValuesFor(kind, name string) []string {
	if name == "variant" {
		if v, ok := variantValues[kind]; ok {
			return v
		}
	}
	if name == "selectable" && kind == "Table" {
		return []string{"none", "single", "multiple"}
	}
	return EnumValues(name)
}

// EnumValues returns the accepted identifiers for an enum property. It is a
// union: a variant that does not apply to a component is simply unused, unless
// that component narrows the list with EnumValuesFor.
func EnumValues(name string) []string {
	if name == "crossAxisAlign" {
		return []string{"start", "center", "end", "stretch"}
	}
	if name == "align" {
		return []string{"start", "center", "end"}
	}
	if name == "mainAxisAlign" {
		return []string{"start", "center", "end", "spaceBetween", "spaceAround", "spaceEvenly"}
	}
	return []string{
		"filled", "tonal", "elevated", "outlined", "text", "plain", "standard",
		"primary", "secondary", "tertiary", "small", "large", "extended",
		"assist", "filter", "input", "suggestion", "horizontal", "vertical",
		"single", "multi",
	}
}
