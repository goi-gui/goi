package tree

import (
	"math"
	"strings"

	"github.com/goi-gui/goi/internal/componentdef"
	"github.com/goi-gui/goi/internal/highlight"
)

// Component holds the state every Material 3 component shares. Concrete
// components embed it and use props(kind) to expose only the properties their
// type accepts.
//
// Interaction state (hover, pressed, focused) is owned by the input layer and
// only ever read by the painter, so it lives here next to the props.
type Component struct {
	NodeBase

	// Flow containers (Toolbar, ButtonGroup, List, ...) lay children out with
	// the same spacing and alignment properties as Row and Column.
	Spacing    Prop[float64]
	CrossAlign Prop[string]
	MainAlign  Prop[string]

	// Content.
	Label       Prop[string]
	Supporting  Prop[string]
	Text        Prop[string]
	Placeholder Prop[string]
	Icon        Prop[string]
	Trailing    Prop[string]
	// IconOn and IconOff override Icon for a Switch's checked state. Both empty
	// means no icon in the thumb.
	IconOn  Prop[string]
	IconOff Prop[string]
	// Language names the syntax a Code block highlights with, and ShowCopy adds
	// the copy button to a Code block's corner.
	Language Prop[string]
	ShowCopy Prop[bool]

	// Appearance.
	Variant Prop[string]
	Size    Prop[string]

	// State.
	Disabled Prop[bool]
	Checked  Prop[bool]
	Selected Prop[bool]
	Expanded Prop[bool]

	// Numeric value (slider, progress, stepper).
	Value Prop[float64]
	Min   Prop[float64]
	Max   Prop[float64]
	Step  Prop[float64]

	// Events.
	Toggle Event[bool]
	Change Event[float64]
	Edit   Event[string]
	Submit Event[string]
	Select Event[int]
	// HoverPoint fires when the pointer moves onto a different point of a
	// chart, with that point's index; -1 is "left the chart".
	HoverPoint Event[int]

	// Runtime interaction state.
	Hover   bool
	Pressed bool
	Focused bool
	// FocusRing is set only when the focus came from the keyboard. A pointer
	// press moves focus too, but M3 only shows the ring for keyboard users.
	FocusRing bool
	// Ripple is the live press ripple: On marks it running, 0..1 is how far it
	// has expanded, and X/Y are its origin in window coordinates.
	RippleOn bool
	Ripple   float64
	RippleX  float64
	RippleY  float64
	// Caret for text fields.
	Caret int
	// Preedit is the IME composition that is not committed yet. It is drawn
	// after the value but is not part of Text until the input method commits.
	Preedit string
	// PreeditCaret is the caret position inside Preedit, in bytes; -1 means the
	// end (the usual case while composing).
	PreeditCaret int
	// TextScroll is how far the value text is shifted left so the caret stays
	// in view. Paint computes it; input reads it to map clicks back to indices.
	TextScroll float64
	// SelAnchor is the fixed end of a text selection; -1 means none.
	SelAnchor int
	// SelectedIndex for tab bars and navigation.
	SelectedIndex int
	// PickerMonth is the month a DatePicker shows, as year*12 + month-1; 0
	// means "the month of the picked date". PickerField is which half of a
	// TimePicker is being set: 0 the hour, 1 the minutes.
	PickerMonth int
	PickerField int

	// TogglePos is where the Switch handle sits, 0 (off) to 1 (on). TogglePosed
	// marks that the ui layer drives it, so paint animates instead of reading
	// Checked directly; a headless paint test leaves it false and gets the end
	// state at once.
	TogglePos   float64
	TogglePosed bool
}

// newComponent seeds the shared defaults.
func newComponent() Component {
	return Component{
		SelAnchor:   -1,
		Label:       newProp(nil, "label", DirtyLayout, ""),
		Supporting:  newProp(nil, "supporting", DirtyLayout, ""),
		Text:        newProp(nil, "text", DirtyLayout, ""),
		Placeholder: newProp(nil, "placeholder", DirtyLayout, ""),
		Icon:        newProp(nil, "icon", DirtyLayout, ""),
		Trailing:    newProp(nil, "trailingIcon", DirtyLayout, ""),
		IconOn:      newProp(nil, "iconOn", DirtyLayout, ""),
		IconOff:     newProp(nil, "iconOff", DirtyLayout, ""),
		Language:    newProp(nil, "language", DirtyPaint, ""),
		ShowCopy:    newProp(nil, "showCopy", DirtyPaint, false),
		Variant:     newProp(nil, "variant", DirtyPaint, ""),
		Size:        newProp(nil, "size", DirtyLayout, ""),
		Disabled:    newProp(nil, "disabled", DirtyPaint, false),
		Checked:     newProp(nil, "checked", DirtyPaint, false),
		// A selected chip or segment shows a check in place of its leading icon,
		// which changes its width, so this one is a layout change.
		Selected:   newProp(nil, "selected", DirtyLayout, false),
		Expanded:   newProp(nil, "expanded", DirtyLayout, false),
		Value:      newProp(nil, "value", DirtyPaint, 0.0),
		Min:        newProp(nil, "min", DirtyPaint, 0.0),
		Max:        newProp(nil, "max", DirtyPaint, 1.0),
		Step:       newProp(nil, "step", DirtyPaint, 0.0),
		Spacing:    newProp(nil, "spacing", DirtyLayout, 0.0),
		CrossAlign: newProp(nil, "crossAxisAlign", DirtyLayout, "start"),
		MainAlign:  newProp(nil, "mainAxisAlign", DirtyLayout, "start"),
	}
}

// namedProps returns the properties listed in propsFor(kind), in that order.
func (c *Component) props(kind string) []NamedProp {
	names := propsFor(kind)
	out := make([]NamedProp, 0, len(names)+14)
	out = append(out, commonProps(&c.NodeBase)...)
	for _, n := range names {
		out = append(out, c.one(kind, n))
	}
	return out
}

// propsExcept is props with some names left to the concrete type, which owns
// cells the shared Component does not have. componentdef still lists those
// names, so the schema and the docs see them.
func (c *Component) propsExcept(kind string, skip ...string) []NamedProp {
	names := propsFor(kind)
	out := make([]NamedProp, 0, len(names)+16)
	out = append(out, commonProps(&c.NodeBase)...)
	for _, n := range names {
		own := false
		for _, s := range skip {
			if n == s {
				own = true
				break
			}
		}
		if own {
			continue
		}
		out = append(out, c.one(kind, n))
	}
	return out
}

// one builds the named property for a single shared field. kind lets an enum
// be narrowed to the values that component implements.
func (c *Component) one(kind, name string) NamedProp {
	b := &c.NodeBase
	switch name {
	case "label":
		return StringProp(b, "label", &c.Label)
	case "supporting":
		return StringProp(b, "supporting", &c.Supporting)
	case "text":
		return StringProp(b, "text", &c.Text)
	case "placeholder":
		return StringProp(b, "placeholder", &c.Placeholder)
	case "icon":
		return StringProp(b, "icon", &c.Icon)
	case "trailingIcon":
		return StringProp(b, "trailingIcon", &c.Trailing)
	case "iconOn":
		return StringProp(b, "iconOn", &c.IconOn)
	case "iconOff":
		return StringProp(b, "iconOff", &c.IconOff)
	case "language":
		return StringProp(b, "language", &c.Language)
	case "showCopy":
		return BoolProp(b, "showCopy", DirtyPaint, &c.ShowCopy)
	case "variant":
		// The accepted words live in componentdef, which the schema validates
		// against. Repeating them here let the validator accept `vertical` on a
		// Divider while the node rejected it, so the enum is read once.
		return EnumProp(b, "variant", DirtyPaint, &c.Variant, componentdef.EnumValuesFor(kind, "variant")...)
	case "size":
		return EnumProp(b, "size", DirtyLayout, &c.Size, componentdef.EnumValues("size")...)
	case "disabled":
		return BoolProp(b, "disabled", DirtyPaint, &c.Disabled)
	case "checked":
		return BoolProp(b, "checked", DirtyPaint, &c.Checked)
	case "selected":
		// A selected chip or segment swaps its leading icon for a check, which
		// changes its width, so this is a layout change rather than a repaint.
		return BoolProp(b, "selected", DirtyLayout, &c.Selected)
	case "expanded":
		return BoolProp(b, "expanded", DirtyLayout, &c.Expanded)
	case "value":
		return NumberProp(b, "value", DirtyPaint, &c.Value)
	case "min":
		return NumberProp(b, "min", DirtyLayout, &c.Min)
	case "max":
		return NumberProp(b, "max", DirtyLayout, &c.Max)
	case "step":
		return NumberProp(b, "step", DirtyLayout, &c.Step)
	case "spacing":
		return NumberProp(b, "spacing", DirtyLayout, &c.Spacing)
	case "gap":
		// gap is the same cell as spacing, under the name a layout reader
		// expects. Whichever the file writes last wins.
		return NumberProp(b, "gap", DirtyLayout, &c.Spacing)
	case "crossAxisAlign":
		return EnumProp(b, "crossAxisAlign", DirtyLayout, &c.CrossAlign, CrossAxisValues...)
	case "mainAxisAlign":
		return EnumProp(b, "mainAxisAlign", DirtyLayout, &c.MainAlign, MainAxisValues...)
	}
	return StringProp(b, name, &c.Label)
}

// propsFor lists the shared M3 properties each component accepts.
func propsFor(kind string) []string { return componentdef.Props(kind) }

// The concrete component types. Each is a distinct Go type so ByID is
// type-checked, and each is otherwise empty: all state lives in Component.

// Button is a filled, tonal, elevated, outlined or text button.
type Button struct{ Component }

// Kind implements Node.
func (*Button) Kind() string              { return "Button" }
func (c *Button) namedProps() []NamedProp { return c.props("Button") }

// IconButton is a button that shows only an icon.
type IconButton struct{ Component }

// Kind implements Node.
func (*IconButton) Kind() string              { return "IconButton" }
func (c *IconButton) namedProps() []NamedProp { return c.props("IconButton") }

// FAB is a floating action button.
type FAB struct{ Component }

// Kind implements Node.
func (*FAB) Kind() string              { return "FAB" }
func (c *FAB) namedProps() []NamedProp { return c.props("FAB") }

// FABMenu opens related actions from a FAB.
type FABMenu struct{ Component }

// Kind implements Node.
func (*FABMenu) Kind() string              { return "FABMenu" }
func (c *FABMenu) namedProps() []NamedProp { return c.props("FABMenu") }

// ButtonGroup is a connected group of buttons.
type ButtonGroup struct{ Component }

// Kind implements Node.
func (*ButtonGroup) Kind() string              { return "ButtonGroup" }
func (c *ButtonGroup) namedProps() []NamedProp { return c.props("ButtonGroup") }

// SplitButton is a primary action plus a menu.
type SplitButton struct{ Component }

// Kind implements Node.
func (*SplitButton) Kind() string              { return "SplitButton" }
func (c *SplitButton) namedProps() []NamedProp { return c.props("SplitButton") }

// SegmentedButton is one segment of a segmented button group.
type SegmentedButton struct{ Component }

// Kind implements Node.
func (*SegmentedButton) Kind() string              { return "SegmentedButton" }
func (c *SegmentedButton) namedProps() []NamedProp { return c.props("SegmentedButton") }

// Chip is an assist, filter, input or suggestion chip.
type Chip struct{ Component }

// Kind implements Node.
func (*Chip) Kind() string              { return "Chip" }
func (c *Chip) namedProps() []NamedProp { return c.props("Chip") }

// Badge is a small count or status dot.
type Badge struct{ Component }

// Kind implements Node.
func (*Badge) Kind() string              { return "Badge" }
func (c *Badge) namedProps() []NamedProp { return c.props("Badge") }

// Checkbox selects one or more items.
type Checkbox struct{ Component }

// Kind implements Node.
func (*Checkbox) Kind() string              { return "Checkbox" }
func (c *Checkbox) namedProps() []NamedProp { return c.props("Checkbox") }

// RadioButton selects one item from a set.
type RadioButton struct{ Component }

// Kind implements Node.
func (*RadioButton) Kind() string              { return "RadioButton" }
func (c *RadioButton) namedProps() []NamedProp { return c.props("RadioButton") }

// Switch turns an option on or off.
type Switch struct{ Component }

// Kind implements Node.
func (*Switch) Kind() string              { return "Switch" }
func (c *Switch) namedProps() []NamedProp { return c.props("Switch") }

// Slider picks a value along a track. `variant: range` adds End as the second
// handle, and `variant: centered` fills from Center instead of the minimum.
type Slider struct {
	Component
	End Prop[float64]
	// Center is NaN when unset, which means the midpoint of min and max.
	Center Prop[float64]
	// Grab is the handle the pointer took: 0 is Value, 1 is End. Paint uses it
	// to narrow only the handle being dragged.
	Grab int
	// RangeEvent fires in range mode with both values, low first.
	RangeEvent Event[[2]float64]
}

// Kind implements Node.
func (*Slider) Kind() string { return "Slider" }

func (c *Slider) namedProps() []NamedProp {
	return append(c.propsExcept("Slider", "end", "center"),
		NumberProp(&c.NodeBase, "end", DirtyPaint, &c.End),
		NumberProp(&c.NodeBase, "center", DirtyPaint, &c.Center),
	)
}

// Range reports the two values of a range slider, low first.
func (c *Slider) Range() (float64, float64) {
	a, b := c.Value.Get(), c.End.Get()
	if a > b {
		return b, a
	}
	return a, b
}

// SetRange sets both handles, clamped to min/max and kept in order.
func (c *Slider) SetRange(a, b float64) {
	lo, hi := c.Min.Get(), c.Max.Get()
	if hi <= lo {
		return
	}
	if a > b {
		a, b = b, a
	}
	c.Value.Set(math.Max(lo, math.Min(hi, a)))
	c.End.Set(math.Max(lo, math.Min(hi, b)))
}

// OnRange subscribes to a range slider's changes, with both values, low first.
func (c *Slider) OnRange(fn func(lo, hi float64)) (off func()) {
	return c.RangeEvent.On(func(v [2]float64) { fn(v[0], v[1]) })
}

// TextField is a filled or outlined text input.
type TextField struct{ Component }

// Kind implements Node.
func (*TextField) Kind() string              { return "TextField" }
func (c *TextField) namedProps() []NamedProp { return c.props("TextField") }

// SearchBar is a rounded search input.
type SearchBar struct{ Component }

// Kind implements Node.
func (*SearchBar) Kind() string              { return "SearchBar" }
func (c *SearchBar) namedProps() []NamedProp { return c.props("SearchBar") }

// Tooltip is a small label attached to a control.
type Tooltip struct{ Component }

// Kind implements Node.
func (*Tooltip) Kind() string              { return "Tooltip" }
func (c *Tooltip) namedProps() []NamedProp { return c.props("Tooltip") }

// Card groups content about a single subject.
type Card struct{ Component }

// Kind implements Node.
func (*Card) Kind() string              { return "Card" }
func (c *Card) namedProps() []NamedProp { return c.props("Card") }

// Dialog asks for a decision in a modal layer.
type Dialog struct{ Component }

// Kind implements Node.
func (*Dialog) Kind() string              { return "Dialog" }
func (c *Dialog) namedProps() []NamedProp { return c.props("Dialog") }

// BottomSheet is a surface anchored to the bottom edge.
type BottomSheet struct{ Component }

// Kind implements Node.
func (*BottomSheet) Kind() string              { return "BottomSheet" }
func (c *BottomSheet) namedProps() []NamedProp { return c.props("BottomSheet") }

// SideSheet is a surface anchored to a side edge.
type SideSheet struct{ Component }

// Kind implements Node.
func (*SideSheet) Kind() string              { return "SideSheet" }
func (c *SideSheet) namedProps() []NamedProp { return c.props("SideSheet") }

// Divider separates content.
type Divider struct{ Component }

// Kind implements Node.
func (*Divider) Kind() string              { return "Divider" }
func (c *Divider) namedProps() []NamedProp { return c.props("Divider") }

// List is a vertical index of items.
type List struct{ Component }

// Kind implements Node.
func (*List) Kind() string              { return "List" }
func (c *List) namedProps() []NamedProp { return c.props("List") }

// ListItem is one row of a list.
type ListItem struct{ Component }

// Kind implements Node.
func (*ListItem) Kind() string              { return "ListItem" }
func (c *ListItem) namedProps() []NamedProp { return c.props("ListItem") }

// Table is a data grid: TableColumn children declare the columns, TableRow
// children hold the cells, and the header sorts on click.
type Table struct {
	Component
	Striped       Prop[bool]
	ShowHeader    Prop[bool]
	StickyHeader  Prop[bool]
	RowHeight     Prop[float64]
	HeaderHeight  Prop[float64]
	SortColumn    Prop[float64]
	SortAscending Prop[bool]
	// Selectable is none, single or multiple; none is the default.
	Selectable  Prop[string]
	SelectedRow Prop[float64]
	// SortEvent fires when a header is clicked, with the column index and the
	// new direction. SortBy from Go does not fire it, because the caller
	// already knows.
	SortEvent Event[SortOrder]
}

// Kind implements Node.
func (*Table) Kind() string { return "Table" }

func (c *Table) namedProps() []NamedProp {
	return append(c.propsExcept("Table",
		"striped", "showHeader", "stickyHeader", "rowHeight", "headerHeight",
		"sortColumn", "sortAscending", "selectable", "selectedRow"),
		BoolProp(&c.NodeBase, "striped", DirtyPaint, &c.Striped),
		BoolProp(&c.NodeBase, "showHeader", DirtyLayout, &c.ShowHeader),
		BoolProp(&c.NodeBase, "stickyHeader", DirtyLayout, &c.StickyHeader),
		NumberProp(&c.NodeBase, "rowHeight", DirtyLayout, &c.RowHeight),
		NumberProp(&c.NodeBase, "headerHeight", DirtyLayout, &c.HeaderHeight),
		NumberProp(&c.NodeBase, "sortColumn", DirtyLayout, &c.SortColumn),
		BoolProp(&c.NodeBase, "sortAscending", DirtyLayout, &c.SortAscending),
		EnumProp(&c.NodeBase, "selectable", DirtyPaint, &c.Selectable, "none", "single", "multiple"),
		NumberProp(&c.NodeBase, "selectedRow", DirtyPaint, &c.SelectedRow),
	)
}

// SortOrder names a table sort: which column, by its key when it has one, and
// the direction.
type SortOrder struct {
	Column    int
	Key       string
	Ascending bool
}

// OnSort subscribes to header clicks.
func (c *Table) OnSort(fn func(SortOrder)) (off func()) { return c.SortEvent.On(fn) }

// TableColumn declares one column: its label, the key Go can sort by, its width
// (0 shares the leftover space), alignment and whether the header sorts.
type TableColumn struct {
	Component
	Key      Prop[string]
	Align    Prop[string]
	Sortable Prop[bool]
	Numeric  Prop[bool]
}

// Kind implements Node.
func (*TableColumn) Kind() string { return "TableColumn" }

func (c *TableColumn) namedProps() []NamedProp {
	return append(c.propsExcept("TableColumn", "key", "align", "sortable", "numeric"),
		StringProp(&c.NodeBase, "key", &c.Key),
		EnumProp(&c.NodeBase, "align", DirtyPaint, &c.Align, "start", "center", "end"),
		BoolProp(&c.NodeBase, "sortable", DirtyPaint, &c.Sortable),
		BoolProp(&c.NodeBase, "numeric", DirtyPaint, &c.Numeric),
	)
}

// TableRow is one row; its children are TableCells, one per column.
type TableRow struct{ Component }

// Kind implements Node.
func (*TableRow) Kind() string { return "TableRow" }

func (c *TableRow) namedProps() []NamedProp { return c.props("TableRow") }

// TableCell is one cell in a row. Its children are what a reader sees, so the
// cell can hold a Text, a Chip, anything; Value is the machine-readable string
// sorting uses, and may differ from what is drawn (`"31"` over a formatted
// date).
type TableCell struct {
	Component
	Value Prop[string]
	Align Prop[string]
}

// Kind implements Node.
func (*TableCell) Kind() string { return "TableCell" }

func (c *TableCell) namedProps() []NamedProp {
	return append(c.propsExcept("TableCell", "value", "align"),
		ScalarProp(&c.NodeBase, "value", DirtyLayout, &c.Value),
		EnumProp(&c.NodeBase, "align", DirtyPaint, &c.Align, "start", "center", "end"),
	)
}

// Menu is a list of actions in an overlay.
type Menu struct{ Component }

// Kind implements Node.
func (*Menu) Kind() string              { return "Menu" }
func (c *Menu) namedProps() []NamedProp { return c.props("Menu") }

// MenuItem is one action in a menu.
type MenuItem struct{ Component }

// Kind implements Node.
func (*MenuItem) Kind() string              { return "MenuItem" }
func (c *MenuItem) namedProps() []NamedProp { return c.props("MenuItem") }

// Snackbar is a short message at the bottom of the screen.
type Snackbar struct{ Component }

// Kind implements Node.
func (*Snackbar) Kind() string              { return "Snackbar" }
func (c *Snackbar) namedProps() []NamedProp { return c.props("Snackbar") }

// TopAppBar is the header of a screen.
type TopAppBar struct{ Component }

// Kind implements Node.
func (*TopAppBar) Kind() string              { return "TopAppBar" }
func (c *TopAppBar) namedProps() []NamedProp { return c.props("TopAppBar") }

// NavigationBar is the bottom navigation for compact windows.
type NavigationBar struct{ Component }

// Kind implements Node.
func (*NavigationBar) Kind() string              { return "NavigationBar" }
func (c *NavigationBar) namedProps() []NamedProp { return c.props("NavigationBar") }

// NavigationRail is the side navigation for medium windows.
type NavigationRail struct{ Component }

// Kind implements Node.
func (*NavigationRail) Kind() string              { return "NavigationRail" }
func (c *NavigationRail) namedProps() []NamedProp { return c.props("NavigationRail") }

// NavigationDrawer is the side panel navigation for expanded windows.
type NavigationDrawer struct{ Component }

// Kind implements Node.
func (*NavigationDrawer) Kind() string              { return "NavigationDrawer" }
func (c *NavigationDrawer) namedProps() []NamedProp { return c.props("NavigationDrawer") }

// NavigationItem is one destination in a bar, rail or drawer.
type NavigationItem struct{ Component }

// Kind implements Node.
func (*NavigationItem) Kind() string              { return "NavigationItem" }
func (c *NavigationItem) namedProps() []NamedProp { return c.props("NavigationItem") }

// Tabs is a row of tabs.
type Tabs struct{ Component }

// Kind implements Node.
func (*Tabs) Kind() string              { return "Tabs" }
func (c *Tabs) namedProps() []NamedProp { return c.props("Tabs") }

// Tab is one tab.
type Tab struct{ Component }

// Kind implements Node.
func (*Tab) Kind() string              { return "Tab" }
func (c *Tab) namedProps() []NamedProp { return c.props("Tab") }

// LinearProgress shows progress along a line.
type LinearProgress struct{ Component }

// Kind implements Node.
func (*LinearProgress) Kind() string              { return "LinearProgress" }
func (c *LinearProgress) namedProps() []NamedProp { return c.props("LinearProgress") }

// CircularProgress shows progress on a ring.
type CircularProgress struct{ Component }

// Kind implements Node.
func (*CircularProgress) Kind() string              { return "CircularProgress" }
func (c *CircularProgress) namedProps() []NamedProp { return c.props("CircularProgress") }

// LoadingIndicator is the expressive loading indicator.
type LoadingIndicator struct{ Component }

// Kind implements Node.
func (*LoadingIndicator) Kind() string              { return "LoadingIndicator" }
func (c *LoadingIndicator) namedProps() []NamedProp { return c.props("LoadingIndicator") }

// DatePicker picks a date.
type DatePicker struct{ Component }

// Kind implements Node.
func (*DatePicker) Kind() string              { return "DatePicker" }
func (c *DatePicker) namedProps() []NamedProp { return c.props("DatePicker") }

// TimePicker picks a time.
type TimePicker struct{ Component }

// Kind implements Node.
func (*TimePicker) Kind() string              { return "TimePicker" }
func (c *TimePicker) namedProps() []NamedProp { return c.props("TimePicker") }

// TextArea is a multiline text field.
type TextArea struct{ Component }

// Kind implements Node.
func (*TextArea) Kind() string              { return "TextArea" }
func (c *TextArea) namedProps() []NamedProp { return c.props("TextArea") }

// Code is a syntax-highlighted code block.
type Code struct {
	Component
	// SelA and SelB are the reader's selection in the drawn text's byte
	// offsets. A Code block is always selectable; only a Text node needs the
	// property, since prose and code are read differently.
	SelA, SelB int
	// The highlight is recomputed only when the text or the language changes,
	// so a still block costs a string compare per frame instead of a lexer run.
	hlLang, hlText string
	hlSrc          string
	hlSpans        []highlight.Span
}

// Kind implements Node.
func (*Code) Kind() string { return "Code" }

func (c *Code) namedProps() []NamedProp { return c.props("Code") }

// Source is the text the block draws. Tabs are expanded here because the font
// gives a tab no advance of its own, which would flatten all indentation; the
// selection and the copy button use the same string, so what is highlighted is
// what is copied.
func (c *Code) Source() string {
	src, _ := c.highlight()
	return src
}

// Spans returns the highlighted runs for the current text and language,
// recomputing them only when one of the two changed.
func (c *Code) Spans() []highlight.Span {
	_, spans := c.highlight()
	return spans
}

func (c *Code) highlight() (string, []highlight.Span) {
	raw, lang := c.Text.Get(), c.Language.Get()
	if raw == c.hlText && lang == c.hlLang {
		return c.hlSrc, c.hlSpans
	}
	c.hlText, c.hlLang = raw, lang
	c.hlSrc = strings.ReplaceAll(raw, "\t", "    ")
	c.hlSpans = highlight.Spans(c.hlSrc, lang)
	return c.hlSrc, c.hlSpans
}

// Carousel shows a scrollable row of items.
type Carousel struct{ Component }

// Kind implements Node.
func (*Carousel) Kind() string              { return "Carousel" }
func (c *Carousel) namedProps() []NamedProp { return c.props("Carousel") }

// Toolbar is a floating row of actions.
type Toolbar struct{ Component }

// Kind implements Node.
func (*Toolbar) Kind() string              { return "Toolbar" }
func (c *Toolbar) namedProps() []NamedProp { return c.props("Toolbar") }

// M3 exposes the shared component state to the painter.
func (c *Component) M3() *Component { return c }
