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
	{"Icon", false},
	{"Code", false},
	{"Terminal", false},
	{"Markdown", false},
	{"Map", true},
	{"Geom", false},
	{"Point", true},
	{"Path", false},
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
	{"Popover", true},
	{"Snackbar", false},
	{"Tooltip", true},
	{"Table", true},
	{"TableColumn", false},
	{"TableRow", true},
	{"TableCell", true},
	{"Carousel", true},
	{"CarouselItem", true},
	// Navigation.
	{"TopAppBar", false},
	{"NavigationBar", true},
	{"NavigationRail", true},
	{"NavigationDrawer", true},
	{"NavigationItem", false},
	{"Breadcrumb", true},
	{"Tabs", true},
	{"Tab", false},
	// Status and progress.
	{"Badge", false},
	{"LinearProgress", false},
	{"CircularProgress", true},
	{"LoadingIndicator", false},
	{"Skeleton", false},
	// Date and time.
	{"DatePicker", true},
	{"TimePicker", true},
	{"ColorPicker", false},
	// Diagrams.
	{"Mermaid", false},
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
	"Button":           {"label", "leadingIcon", "filledIcon", "filledLeadingIcon", "variant", "color", "disabled"},
	"Icon":             {"icon", "filledIcon", "color"},
	"IconButton":       {"icon", "filledIcon", "variant", "disabled", "selected", "iconColor"},
	"FAB":              {"label", "icon", "filledIcon", "variant", "disabled"},
	"FABMenu":          {"icon", "filledIcon", "variant", "disabled", "expanded"},
	"ButtonGroup":      {},
	"SplitButton":      {"label", "leadingIcon", "trailingIcon", "filledIcon", "filledLeadingIcon", "filledTrailingIcon", "variant", "disabled", "expanded"},
	"SegmentedButton":  {"label", "leadingIcon", "filledIcon", "filledLeadingIcon", "variant", "disabled", "selected"},
	"Chip":             {"label", "leadingIcon", "trailingIcon", "filledIcon", "filledLeadingIcon", "filledTrailingIcon", "variant", "disabled", "selected", "labelColor", "leadingIconColor", "trailingIconColor"},
	"Badge":            {"label"},
	"Checkbox":         {"label", "disabled", "checked"},
	"RadioButton":      {"label", "disabled", "checked"},
	"Switch":           {"label", "disabled", "checked", "icon", "iconOn", "iconOff", "filledIcon"},
	"Slider":           {"variant", "disabled", "value", "end", "center", "min", "max", "step", "showTooltip"},
	"TextField":        {"label", "text", "placeholder", "supporting", "leadingIcon", "trailingIcon", "filledIcon", "filledLeadingIcon", "filledTrailingIcon", "variant", "color", "disabled", "type", "mask", "obscureText"},
	"SearchBar":        {"label", "text", "placeholder", "trailingIcon", "filledIcon", "filledTrailingIcon", "disabled"},
	"Tooltip":          {"text"},
	"Card":             {"label", "supporting", "variant", "disabled"},
	"Dialog":           {"label", "supporting", "leadingIcon", "filledIcon", "filledLeadingIcon"},
	"BottomSheet":      {"label"},
	"SideSheet":        {"label"},
	"Divider":          {"variant"},
	"List":             {},
	"ListItem":         {"label", "supporting", "leadingIcon", "trailingIcon", "filledIcon", "filledLeadingIcon", "filledTrailingIcon", "leadingIconColor", "trailingIconColor", "labelColor", "supportingColor", "disabled", "selected"},
	"Table":            {"variant", "striped", "showHeader", "stickyHeader", "rowHeight", "headerHeight", "sortColumn", "sortAscending", "selectable", "selectedRow", "selectionColor"},
	"TableColumn":      {"label", "icon", "filledIcon", "key", "align", "sortable", "numeric"},
	"TableRow":         {"selected", "disabled"},
	"TableCell":        {"value", "icon", "filledIcon", "align"},
	"Menu":             {},
	"MenuItem":         {"label", "leadingIcon", "trailingIcon", "filledIcon", "filledLeadingIcon", "filledTrailingIcon", "disabled", "selected"},
	"Popover":          {"align"},
	"Snackbar":         {"label"},
	"TopAppBar":        {"label", "leadingIcon", "trailingIcon", "filledIcon", "filledLeadingIcon", "filledTrailingIcon"},
	"NavigationBar":    {"variant", "animation"},
	"NavigationRail":   {"animation"},
	"NavigationDrawer": {"label", "animation"},
	"NavigationItem":   {"label", "leadingIcon", "filledIcon", "filledLeadingIcon", "disabled", "selected", "labelColor", "selectedLabelColor", "iconColor", "selectedIconColor", "selectedContainerColor"},
	"Breadcrumb":       {"separator", "separatorColor", "separatorSize"},
	"Tabs":             {"variant"},
	"Tab":              {"label", "leadingIcon", "filledIcon", "filledLeadingIcon", "disabled", "selected"},
	"LinearProgress":   {"variant", "value", "min", "max", "thickness"},
	"CircularProgress": {"variant", "value", "min", "max", "thickness"},
	"LoadingIndicator": {"color"},
	"Skeleton":         {"color", "highlight", "animated"},
	"DatePicker":       {"label", "text"},
	"TimePicker":       {"label", "text", "variant", "hourFormat"},
	"ColorPicker":      {"label", "text"},
	"Carousel":         {"shrink"},
	"CarouselItem":     {"label", "supporting", "labelColor", "supportingColor"},
	"Toolbar":          {},
	"TextArea":         {"label", "text", "placeholder", "supporting", "variant", "disabled"},
	"Code":             {"text", "language", "variant", "showCopy"},
	"Terminal":         {"fontSize", "scrollback", "foreground"},
	"Markdown":         {"text"},
	"Mermaid":          {"data"},
	"Map":              {"tiles", "attribution", "center", "zoom", "bbox", "reverseTileColors", "disablePan", "disableZoom"},
	"Geom":             {"data", "lineColor", "fillColor"},
	"Point":            {"coordinate"},
	"Path":             {"coordinates", "lineColor"},
	"LineChart":        {"grid", "axis", "legend", "categories", "textColor", "gridColor", "values", "smooth", "points", "min", "max"},
	"AreaChart":        {"grid", "axis", "legend", "categories", "textColor", "gridColor", "values", "smooth", "points", "stacked", "min", "max"},
	"BarChart":         {"grid", "axis", "legend", "categories", "textColor", "gridColor", "values", "stacked", "horizontal", "barRadius", "barGap", "barBorderThickness", "barBorderColor", "min", "max"},
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
		"NavigationBar", "NavigationRail", "NavigationDrawer", "Breadcrumb", "List", "Menu",
		"Card", "Dialog", "BottomSheet", "SideSheet", "DatePicker", "TimePicker",
		"Tooltip", "Popover":
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
	// Coords is a list of [lon, lat] pairs, which a Path's coordinates take.
	Coords
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
	// A skeleton shimmers until the design says otherwise.
	"animated": true,
	// A bar takes its corner from the chart's own radius unless the design
	// names one, the way radiusTop falls back to radius.
	"barRadius": -1.0,
	// A map starts on the whole world, drawn from OpenStreetMap, and credits
	// it until the design says otherwise.
	"zoom": 2.0,
	// A breadcrumb's separator is the chevron every file manager uses, at the
	// 18dp an icon beside a label takes.
	"separator":     "chevron_right",
	"separatorSize": 20.0,
	"tiles":         "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
	"attribution":   "© OpenStreetMap contributors",
	// A terminal draws a readable grid unless the design names a size and a
	// scrollback depth.
	"fontSize":   13.0,
	"scrollback": 1000.0,
}

// kindDefaults are defaults that belong to one kind, for a property name that
// means different things elsewhere: a Slider's `center` is a number, a Map's is
// a position string, and neither can take the other's default.
var kindDefaults = map[string]map[string]any{
	"Slider":     {"center": math.NaN()},
	"Tabs":       {"variant": "secondary"},
	"Popover":    {"align": "start"},
	"Breadcrumb": {"spacing": 32.0},
}

// slots lists the child slots a built-in component declares, by bare name. A
// slot is written `$popover { … }`, the way a fragment writes a named slot, but
// it belongs to the component and needs no caller above it.
var slots = map[string][]string{
	"Popover":   {"popover"},
	"ListItem":  {"leading", "trailing"},
	"TextField": {"leading", "trailing"},
	"SearchBar": {"leading", "trailing"},
}

// Slots returns the child slots a kind declares, without the `$`.
func Slots(kind string) []string { return slots[kind] }

// SlotOwner names the kind that declares `$name`, "" when no built-in does.
func SlotOwner(name string) string {
	for kind, names := range slots {
		for _, s := range names {
			if "$"+s == name {
				return kind
			}
		}
	}
	return ""
}

// Slot reports whether a kind declares the child slot `$name`.
func Slot(kind, name string) bool {
	for _, s := range slots[kind] {
		if "$"+s == name {
			return true
		}
	}
	return false
}

// Default returns the initial value of a component property, when it is not the
// zero value for its type.
func Default(kind, name string) (any, bool) {
	if d, ok := kindDefaults[kind]; ok {
		if v, ok := d[name]; ok {
			return v, true
		}
	}
	v, ok := defaults[name]
	return v, ok
}

// TypeOfFor is TypeOf narrowed per component where one name means two types:
// a `value` is a number on a Slider and the sort string on a TableCell.
func TypeOfFor(kind, name string) PropType {
	if kind == "TableCell" && name == "value" {
		return Scalar
	}
	// A Mermaid chart's `data` is source text; a Geom's is coordinate pairs.
	if kind == "Mermaid" && name == "data" {
		return String
	}
	// A Slider's center is a number; a Map's is a `lat,lon` string.
	if kind == "Map" && name == "center" {
		return String
	}
	return TypeOf(name)
}

// TypeOf returns the literal type of a component property.
func TypeOf(name string) PropType {
	switch name {
	case "disabled", "checked", "selected", "expanded", "showCopy", "showTooltip", "animated", "obscureText",
		"filledIcon", "filledLeadingIcon", "filledTrailingIcon",
		"sortable", "numeric", "striped", "showHeader", "stickyHeader", "sortAscending",
		"grid", "axis", "legend", "values", "stacked", "horizontal", "smooth", "points",
		"disablePan", "disableZoom", "reverseTileColors":
		return Bool
	case "value", "end", "center", "min", "max", "step", "spacing", "gap", "size", "hourFormat", "separatorSize",
		"rowHeight", "headerHeight", "sortColumn", "selectedRow",
		"innerRadius", "startAngle", "thickness", "zoom",
		"barRadius", "barGap", "barBorderThickness", "fontSize", "scrollback":
		return Number
	case "variant", "animation", "crossAxisAlign", "mainAxisAlign", "align", "selectable":
		return Enum
	case "textColor", "gridColor", "color", "lineColor", "fillColor", "highlight", "barBorderColor",
		"leadingIconColor", "trailingIconColor", "labelColor", "supportingColor",
		"selectedLabelColor", "selectedIconColor", "selectedContainerColor", "selectionColor",
		"separatorColor", "iconColor", "foreground":
		return Color
	case "categories", "bbox", "data", "coordinate", "coordinates":
		return List
	}
	return String
}

// ListElem is the type of one item of a list property, which is what the
// checker reads each entry as: a chart's categories are labels, a map's bbox is
// four numbers.
func ListElem(name string) PropType {
	switch name {
	case "bbox", "coordinate":
		return Number
	case "data", "coordinates":
		return Coords
	}
	return String
}

// variantValues narrows the shared variant union where a component paints only
// a few styles, so completion and the checker do not offer a word the painter
// would ignore.
var variantValues = map[string][]string{
	"NavigationBar":    {"compact", "medium"},
	"TextArea":         {"filled", "outlined"},
	"Tabs":             {"primary", "secondary"},
	"Table":            {"outlined", "filled", "plain"},
	"TimePicker":       {"dial", "input"},
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
		return tableSelectableValues
	}
	return EnumValues(name)
}

// EnumValues returns the accepted identifiers for an enum property. It is a
// union: a variant that does not apply to a component is simply unused, unless
// that component narrows the list with EnumValuesFor.
// The enum value lists are shared, not rebuilt per node: a component with a
// variant used to copy twenty values into its own cell table.
var (
	alignValues           = []string{"start", "center", "end"}
	crossAxisValues       = []string{"start", "center", "end", "stretch"}
	mainAxisValues        = []string{"start", "center", "end", "spaceBetween", "spaceAround", "spaceEvenly"}
	tableSelectableValues = []string{"none", "single", "multiple"}
	navAnimationValues    = []string{"slide", "fade", "none"}
	variantUnion          = []string{
		"filled", "tonal", "elevated", "outlined", "text", "plain", "standard",
		"primary", "secondary", "tertiary", "small", "large", "extended",
		"assist", "filter", "input", "suggestion", "horizontal", "vertical",
		"single", "multi",
	}
)

func EnumValues(name string) []string {
	switch name {
	case "crossAxisAlign":
		return crossAxisValues
	case "align":
		return alignValues
	case "mainAxisAlign":
		return mainAxisValues
	case "animation":
		return navAnimationValues
	}
	return variantUnion
}
