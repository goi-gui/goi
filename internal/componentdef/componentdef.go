// Package componentdef is the single list of Material 3 component kinds and
// the properties each one accepts. tree builds its property tables from it and
// schema validates against it, so the two cannot drift apart.
package componentdef

// Kind is one component element name.
type Kind struct {
	Name string
	// Container marks components that take child nodes.
	Container bool
}

// Kinds lists every Material 3 component, in the order they appear in docs.
var Kinds = []Kind{
	{"Button", false},
	{"IconButton", false},
	{"FAB", false},
	{"FABMenu", true},
	{"ButtonGroup", true},
	{"SplitButton", false},
	{"SegmentedButton", false},
	{"Chip", false},
	{"Badge", false},
	{"Checkbox", false},
	{"RadioButton", false},
	{"Switch", false},
	{"Slider", false},
	{"TextField", false},
	{"SearchBar", false},
	{"Tooltip", false},
	{"Card", true},
	{"Dialog", true},
	{"BottomSheet", true},
	{"SideSheet", true},
	{"Divider", false},
	{"List", true},
	{"ListItem", false},
	{"Table", true},
	{"TableColumn", false},
	{"TableRow", true},
	{"TableCell", false},
	{"Menu", true},
	{"MenuItem", false},
	{"Snackbar", false},
	{"TopAppBar", false},
	{"NavigationBar", true},
	{"NavigationRail", true},
	{"NavigationDrawer", true},
	{"NavigationItem", false},
	{"Tabs", true},
	{"Tab", false},
	{"LinearProgress", false},
	{"CircularProgress", false},
	{"LoadingIndicator", false},
	{"DatePicker", true},
	{"TimePicker", true},
	{"Carousel", true},
	{"Toolbar", true},
	{"TextArea", false},
	{"Code", false},
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
	"Button":           {"label", "icon", "variant", "size", "disabled"},
	"IconButton":       {"icon", "variant", "size", "disabled", "selected"},
	"FAB":              {"label", "icon", "variant", "size", "disabled", "expanded"},
	"FABMenu":          {"icon", "variant", "size", "disabled", "expanded"},
	"ButtonGroup":      {"variant", "size", "disabled"},
	"SplitButton":      {"label", "icon", "variant", "size", "disabled", "expanded"},
	"SegmentedButton":  {"label", "icon", "variant", "size", "disabled", "selected"},
	"Chip":             {"label", "icon", "trailingIcon", "variant", "size", "disabled", "selected", "checked"},
	"Badge":            {"label", "variant", "size"},
	"Checkbox":         {"label", "variant", "disabled", "checked"},
	"RadioButton":      {"label", "variant", "disabled", "checked"},
	"Switch":           {"label", "variant", "disabled", "checked", "icon", "iconOn", "iconOff"},
	"Slider":           {"variant", "disabled", "value", "min", "max", "step"},
	"TextField":        {"label", "text", "placeholder", "supporting", "icon", "trailingIcon", "variant", "disabled"},
	"SearchBar":        {"label", "text", "placeholder", "icon", "trailingIcon", "variant", "disabled"},
	"Tooltip":          {"label", "variant"},
	"Card":             {"label", "supporting", "icon", "variant", "disabled"},
	"Dialog":           {"label", "supporting", "icon", "variant"},
	"BottomSheet":      {"label", "supporting", "variant"},
	"SideSheet":        {"label", "supporting", "variant"},
	"Divider":          {"variant"},
	"List":             {"variant"},
	"ListItem":         {"label", "supporting", "icon", "trailingIcon", "variant", "disabled", "selected"},
	"Table":            {"variant", "striped", "showHeader", "rowHeight", "headerHeight", "sortColumn", "sortAscending", "selectedRow"},
	"TableColumn":      {"label", "key", "align", "sortable", "numeric"},
	"TableRow":         {"selected", "disabled"},
	"TableCell":        {"text", "icon", "align"},
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
	"LinearProgress":   {"variant", "value", "min", "max"},
	"CircularProgress": {"variant", "value", "min", "max", "size"},
	"LoadingIndicator": {"variant", "size"},
	"DatePicker":       {"label", "text", "variant", "size"},
	"TimePicker":       {"label", "text", "variant", "size"},
	"Carousel":         {"variant"},
	"Toolbar":          {"variant", "size"},
	"TextArea":         {"label", "text", "placeholder", "supporting", "variant", "disabled"},
	"Code":             {"text", "language", "variant", "showCopy"},
}

// Flow lists containers that accept the row/column layout properties.
func Flow(kind string) bool {
	switch kind {
	case "ButtonGroup", "Toolbar", "Carousel", "FABMenu", "Tabs",
		"NavigationBar", "NavigationRail", "NavigationDrawer", "List", "Menu",
		"Card", "Dialog", "BottomSheet", "SideSheet", "DatePicker", "TimePicker":
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
)

// defaults are the component properties whose initial value is not the Go zero
// value. The schema applies them, so the builder and the validator agree on
// what a property means before the design writes it.
var defaults = map[string]any{
	"showHeader":    true,
	"rowHeight":     48.0,
	"headerHeight":  40.0,
	"sortColumn":    -1.0,
	"sortAscending": true,
	"selectedRow":   -1.0,
}

// Default returns the initial value of a component property, when it is not the
// zero value for its type.
func Default(name string) (any, bool) {
	v, ok := defaults[name]
	return v, ok
}

// TypeOf returns the literal type of a component property.
func TypeOf(name string) PropType {
	switch name {
	case "disabled", "checked", "selected", "expanded", "showCopy",
		"sortable", "numeric", "striped", "showHeader", "sortAscending":
		return Bool
	case "value", "min", "max", "step", "spacing", "gap",
		"rowHeight", "headerHeight", "sortColumn", "selectedRow":
		return Number
	case "variant", "size", "crossAxisAlign", "mainAxisAlign", "align":
		return Enum
	}
	return String
}

// EnumValues returns the accepted identifiers for an enum property. It is a
// union: a variant that does not apply to a component is simply unused.
func EnumValues(name string) []string {
	if name == "size" {
		return []string{"xs", "sm", "md", "lg", "xl", "small", "large", "extended", "primary", "secondary"}
	}
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
