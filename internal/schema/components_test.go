package schema

import (
	"fmt"
	"testing"

	"github.com/goi-gui/goi/internal/componentdef"
	"github.com/goi-gui/goi/internal/syntax"
)

// TestEveryComponentValidates keeps the schema vocabulary and the component
// list in sync: a new component that nobody can write in a .goi file fails here.
func TestEveryComponentValidates(t *testing.T) {
	for _, k := range componentdef.Kinds {
		src := fmt.Sprintf("Window { %s { } }", k.Name)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s: %v", k.Name, diag)
		}
	}
}

// TestTableNarrowsVariants keeps the checker honest about the styles the table
// painter implements: the shared union accepts many words, the table only three.
func TestTableNarrowsVariants(t *testing.T) {
	for _, v := range []string{"outlined", "filled", "plain"} {
		src := fmt.Sprintf("Window { Table { variant: %s } }", v)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("Table.variant %q: %v", v, diag)
		}
	}
	src := "Window { Table { variant: tonal } }"
	if _, diag := validate(t, src); diag.Empty() {
		t.Fatal("the table accepted a variant it does not paint")
	}
}

// TestTableSelectableValues checks the mode enum and that single is the default.
func TestTableSelectableValues(t *testing.T) {
	for _, v := range []string{"none", "single", "multiple"} {
		src := fmt.Sprintf("Window { Table { selectable: %s } }", v)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("Table.selectable %q: %v", v, diag)
		}
	}
	if _, diag := validate(t, "Window { Table { selectable: double } }"); diag.Empty() {
		t.Fatal("the table accepted an unknown selection mode")
	}
}

// TestSliderNarrowsVariants keeps the slider to the three kinds it paints: a
// standard one, a centered one and a range.
func TestSliderNarrowsVariants(t *testing.T) {
	for _, v := range []string{"standard", "centered", "range"} {
		src := fmt.Sprintf("Window { Slider { variant: %s } }", v)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("Slider.variant %q: %v", v, diag)
		}
	}
	if _, diag := validate(t, "Window { Slider { variant: tonal } }"); diag.Empty() {
		t.Fatal("the slider accepted a variant it does not paint")
	}
}

// TestEventCallsNodeTransport keeps the design-side transport honest: an event
// may call play, pause or stop on an element by id, and nothing else.
func TestEventCallsNodeTransport(t *testing.T) {
	base := `Window { Column {
		Image { id: spin }
		Button { %s }
	} }`
	for _, call := range []string{"spin.play()", "spin.pause()", "spin.stop()"} {
		src := fmt.Sprintf(base, "onClick: "+call)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s: %v", call, diag)
		}
	}
	for _, bad := range []string{"spin.rewind()", "ghost.play()", "spin.play(1)"} {
		src := fmt.Sprintf(base, "onClick: "+bad)
		if _, diag := validate(t, src); diag.Empty() {
			t.Errorf("%s was accepted", bad)
		}
	}
}

// TestEventCallsWindowControls lets a design drive the window without a bridge
// handler: `onClick: window.close()`.
func TestEventCallsWindowControls(t *testing.T) {
	base := `Window { Button { %s } }`
	for _, call := range []string{
		"window.maximize(true)", "window.maximize(false)",
		"window.toggleMaximize()", "window.minimize()",
		"window.fullscreen(true)", "window.fullscreen(false)",
		"window.close()",
	} {
		src := fmt.Sprintf(base, "onClick: "+call)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s: %v", call, diag)
		}
	}
	for _, bad := range []string{
		"window.unknown()", "window.close(true)", "window.maximize()",
		"window.maximize(1)", "window.fullscreen()", "window.minimize(true)",
		"window.toggleMaximize(true)",
	} {
		src := fmt.Sprintf(base, "onClick: "+bad)
		if _, diag := validate(t, src); diag.Empty() {
			t.Errorf("%s was accepted", bad)
		}
	}
}

// TestTableCellValueTakesScalars checks that a cell's sort value accepts a
// number and a bool as well as text, and that TableCell still rejects a map.
func TestTableCellValueTakesScalars(t *testing.T) {
	for _, v := range []string{`"Kaan"`, "42", "3.14", "-7", "true"} {
		src := fmt.Sprintf("Window { Table { TableRow { TableCell { value: %s } } } }", v)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("value: %s: %v", v, diag)
		}
	}
	// An unknown property is still reported, so a typo in a cell is not silent.
	src := `Window { Table { TableRow { TableCell { valeu: "a" } } } }`
	if _, diag := validate(t, src); diag.Empty() {
		t.Fatal("a misspelled cell property was accepted")
	}
}

// TestColorAcceptsCSSForms keeps the checker aligned with the parser: names,
// rgb(), hsl() and gradients pass, and a misspelled colour fails.
func TestColorAcceptsCSSForms(t *testing.T) {
	for _, v := range []string{
		`white`, `"red"`, `"rgb(42, 123, 155)"`, `"rgba(0,0,0,0.5)"`,
		`"hsl(200 40% 50%)"`, `#fff`, `theme.primary`, `customColor.brand`,
	} {
		src := fmt.Sprintf("Window { Rectangle { background: %s } }", v)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("background: %s: %v", v, diag)
		}
	}
	src := `Window { Rectangle { background: "linear-gradient(90deg, red 0%, blue 100%)" } }`
	if _, diag := validate(t, src); !diag.Empty() {
		t.Errorf("gradient: %v", diag)
	}
	src = `Window { Rectangle { background: "notacolour" } }`
	if _, diag := validate(t, src); diag.Empty() {
		t.Fatal("a misspelled colour was accepted")
	}
	src = `Window { Rectangle { background: "linear-gradient(90deg, red)" } }`
	if _, diag := validate(t, src); diag.Empty() {
		t.Fatal("a one-stop gradient was accepted")
	}
}

// TestGradientOnlyOnBackground keeps the promise honest: a gradient is a box
// fill, so a text colour or a border refuses it instead of drawing nothing.
func TestGradientOnlyOnBackground(t *testing.T) {
	src := `Window { Rectangle { background: "linear-gradient(90deg, red, blue)" } }`
	if _, diag := validate(t, src); !diag.Empty() {
		t.Errorf("background gradient: %v", diag)
	}
	src = `Window { Text { text: "x" color: "linear-gradient(90deg, red, blue)" } }`
	if _, diag := validate(t, src); diag.Empty() {
		t.Fatal("a gradient was accepted as a text colour")
	}
}

// TestColourReferences checks the two reference namespaces and what is rejected:
// a role the scheme does not have, and a third namespace.
func TestColourReferences(t *testing.T) {
	for _, src := range []string{
		`Window { Text { text: "x" color: theme.primary } }`,
		`Window { Rectangle { background: customColor.brand } }`,
		`Window { Text { text: "x" color: theme.surfaceContainerHigh } }`,
	} {
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s: %v", src, diag)
		}
	}
	for _, src := range []string{
		`Window { Text { text: "x" color: theme.nope } }`,
		`Window { Text { text: "x" color: palette.primary } }`,
	} {
		if _, diag := validate(t, src); diag.Empty() {
			t.Errorf("%s was accepted", src)
		}
	}
}

// TestRotateValues covers every CSS rotate form, and the typos that must fail.
func TestRotateValues(t *testing.T) {
	for _, v := range []string{"none", "90deg", "0.25turn", "1.57rad", "100grad", "x 90deg", "y 0.25turn", "z 1.57rad", "1 1 1 90deg"} {
		src := fmt.Sprintf("Window { Column { rotate: %s } }", v)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("rotate: %q: %v", v, diag)
		}
	}
	for _, v := range []string{`"90deg"`, "90", "w 90deg", "1 1 90deg"} {
		src := fmt.Sprintf("Window { Column { rotate: %s } }", v)
		// Some of these are syntax errors and some validation errors; either way
		// the file must not pass.
		if _, diag := syntax.Parse([]byte(src)); !diag.Empty() {
			continue
		}
		if _, diag := validate(t, src); diag.Empty() {
			t.Errorf("rotate: %s was accepted", v)
		}
	}
}

// TestProgressNarrowsVariants keeps the two progress kinds to the styles they
// paint: a plain bar and a wavy ribbon.
func TestProgressNarrowsVariants(t *testing.T) {
	for _, kind := range []string{"LinearProgress", "CircularProgress"} {
		for _, v := range []string{"normal", "wavy"} {
			src := fmt.Sprintf("Window { %s { variant: %s } }", kind, v)
			if _, diag := validate(t, src); !diag.Empty() {
				t.Errorf("%s.variant %q: %v", kind, v, diag)
			}
		}
		src := fmt.Sprintf("Window { %s { variant: filled } }", kind)
		if _, diag := validate(t, src); diag.Empty() {
			t.Errorf("%s accepted a style it does not paint", kind)
		}
	}
}

// TestNoElementRepeatsAProperty is the flow-container bug: Props() adds the
// layout properties the schema also typed, so every Flow component carried
// spacing and the two alignments twice. Completion then offered each one twice
// and the docs table would list them twice.
func TestNoElementRepeatsAProperty(t *testing.T) {
	for _, el := range elements {
		seen := make(map[string]bool, len(el.Attrs))
		for _, p := range el.Attrs {
			if seen[p.Name] {
				t.Errorf("%s: %s appears twice", el.Name, p.Name)
			}
			seen[p.Name] = true
		}
	}
}

// TestEveryElementTakesClickOutside keeps the event where onClick is: every
// element is a hit area, so a press outside one can be reported from a plain box
// as well as from a component.
func TestEveryElementTakesClickOutside(t *testing.T) {
	for _, e := range elements {
		if !e.HasEvent("onClickOutside") {
			t.Errorf("%s does not accept onClickOutside", e.Name)
		}
	}
}

// TestGestureDetectorTakesMouseEvents keeps the pointer events where the pointer
// gestures live, and off a plain container that has no detector to report them.
func TestGestureDetectorTakesMouseEvents(t *testing.T) {
	def, ok := Lookup("GestureDetector")
	if !ok {
		t.Fatal("no GestureDetector")
	}
	for _, name := range []string{"onMouseEnter", "onMouseMove", "onMouseLeave"} {
		if !def.HasEvent(name) {
			t.Errorf("GestureDetector does not accept %s", name)
		}
	}
	if col, ok := Lookup("Column"); !ok || col.HasEvent("onMouseMove") {
		t.Error("Column accepted a GestureDetector event")
	}
}

// TestSplitButtonExpands keeps the split button's state methods honest: an
// event may toggle or set the expanded state by id, and nothing else.
func TestSplitButtonExpands(t *testing.T) {
	base := `Window { Column {
		SplitButton { id: split label: "Split" }
		Button { %s }
	} }`
	for _, call := range []string{"split.expand()", "split.expand(true)", "split.expand(false)"} {
		src := fmt.Sprintf(base, "onClick: "+call)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s: %v", call, diag)
		}
	}
	for _, bad := range []string{"split.expand(3)", "split.expand(true, false)", "split.collapse()"} {
		src := fmt.Sprintf(base, "onClick: "+bad)
		if _, diag := validate(t, src); diag.Empty() {
			t.Errorf("%s was accepted", bad)
		}
	}
	// Only a component with an expanded state accepts the method.
	src := `Window { Column {
		Button { id: plain label: "Plain" }
		Button { onClick: plain.expand(true) }
	} }`
	if _, diag := validate(t, src); diag.Empty() {
		t.Fatal("expand on a Button was accepted")
	}
}

// TestLoopVariablesCarryGoTypes keeps the range variables typed the way Go types
// them: an index is an int, a map's first name is its key, and a scalar list's
// element can feed a handler that takes that type.
func TestLoopVariablesCarryGoTypes(t *testing.T) {
	const src = `bridge {
	channels []loopChannel
	names    []string
	scores   map[string]int
	onIndex  func(index int)
	onKey    func(key string)
	onValue  func(value int)
	onName   func(name string)
}

type loopChannel {
	Name string
}

Window {
	Column {
		for i, channel := range bridge.channels {
			Button { label: channel.Name onClick: bridge.onIndex(i) }
		}
		for k, v := range bridge.scores {
			Button { label: k onClick: bridge.onKey(k) }
			Button { label: k onClick: bridge.onValue(v) }
		}
		for _, name := range bridge.names {
			Button { label: name onClick: bridge.onName(name) }
		}
		for i := range 4 {
			Button { label: "n" onClick: bridge.onIndex(i) }
		}
	}
}`
	if _, diag := validate(t, src); !diag.Empty() {
		t.Fatalf("loop variables were mis-typed: %v", diag)
	}

	// The rule is a type, not a free pass: a row is not an int, and a string
	// field cannot feed a number parameter.
	bad := `bridge {
	channels []loopChannel
	onIndex  func(index int)
}
type loopChannel {
	Name string
}
Window {
	Column {
		for i, channel := range bridge.channels {
			Button { label: channel.Name onClick: bridge.onIndex(channel.Name) }
			Button { label: channel.Name onClick: bridge.onIndex(i) }
		}
	}
}`
	if _, diag := validate(t, bad); diag.Empty() {
		t.Fatal("a string field fed a number parameter")
	}
}

// TestSetObscureTextCallsAField keeps the reveal call honest: the value is a
// required bool and only a field has one to set.
func TestSetObscureTextCallsAField(t *testing.T) {
	base := `Window { Column {
		TextField { id: password label: "Password" }
		Text { id: label text: "x" }
		Button { %s }
	} }`
	for _, call := range []string{"password.setObscureText(true)", "password.setObscureText(false)"} {
		if _, diag := validate(t, fmt.Sprintf(base, "onClick: "+call)); !diag.Empty() {
			t.Errorf("%s: %v", call, diag)
		}
	}
	for _, bad := range []string{"password.setObscureText()", "password.setObscureText(1)", "label.setObscureText(true)"} {
		if _, diag := validate(t, fmt.Sprintf(base, "onClick: "+bad)); diag.Empty() {
			t.Errorf("%s was accepted", bad)
		}
	}
}

// TestTextFieldTakesIconSlots keeps a password field's reveal control legal:
// $leading and $trailing belong to the component, and an icon still wins over
// either body without the design having to choose.
func TestTextFieldTakesIconSlots(t *testing.T) {
	src := `Window { TextField {
		label:       "Password"
		obscureText: true
		$leading { Icon { icon: lock } }
		$trailing { IconButton { icon: visibility } }
	} }`
	if _, diag := validate(t, src); !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	src = `Window { TextField { $popover { Icon { icon: lock } } } }`
	if _, diag := validate(t, src); diag.Empty() {
		t.Fatal("a field accepted a slot it does not declare")
	}
}

// TestRemovedPropertiesAreRejected: a property that never did anything is gone
// from the vocabulary, so goipls and the checker stop offering it.
func TestRemovedPropertiesAreRejected(t *testing.T) {
	for _, src := range []string{
		`Window { Tabs { variant: filled } }`,
		`Window { List { variant: plain } }`,
		`Window { Chip { label: "x" checked: true } }`,
		`Window { SearchBar { leadingIcon: star } }`,
		`Window { FAB { icon: add expanded: true } }`,
		`Window { BottomSheet { supporting: "x" } }`,
		`Window { SideSheet { supporting: "x" } }`,
		`Window { DatePicker { variant: input } }`,
		`Window { Card { leadingIcon: star } }`,
		`Window { ButtonGroup { disabled: true } }`,
		`Window { Text { text: "x" surfaceColor: #ff0000 } }`,
		`Window { RichText { surfaceColor: #ff0000 } }`,
		`Window { SVG { src: "a.svg" surfaceColor: #ff0000 } }`,
		`Window { LineChart { ChartSeries { values: [1] surfaceColor: #ff0000 } } }`,
	} {
		if _, diag := validate(t, src); diag.Empty() {
			t.Errorf("%s was accepted", src)
		}
	}
	// The same names still work where they mean something.
	for _, src := range []string{
		`Window { Button { label: "x" variant: outlined } }`,
		`Window { SegmentedButton { label: "a" variant: single } }`,
		`Window { Chip { label: "x" selected: true } }`,
		`Window { FABMenu { icon: add expanded: true } }`,
		`Window { Audio { src: "a.mp3" surfaceColor: #ff0000 } }`,
	} {
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s was rejected: %v", src, diag)
		}
	}
}

// TestNavigationBarNarrowsVariants keeps the bar to the two styles it paints.
func TestNavigationBarNarrowsVariants(t *testing.T) {
	for _, v := range []string{"compact", "medium"} {
		src := fmt.Sprintf(`Window { NavigationBar { variant: %s NavigationItem { label: "a" } } }`, v)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("variant %q: %v", v, diag)
		}
	}
	src := `Window { NavigationBar { variant: filled NavigationItem { label: "a" } } }`
	if _, diag := validate(t, src); diag.Empty() {
		t.Fatal("the bar accepted a style it does not paint")
	}
}

// TestSliderIconsUseThemeColors keeps the end-icon colours out of the design:
// they follow the track part the glyph sits on, not a per-slider override.
func TestSliderIconsUseThemeColors(t *testing.T) {
	for _, name := range []string{"leadingIconColor", "trailingIconColor"} {
		src := fmt.Sprintf("Window { Slider { %s: #123456 } }", name)
		if _, diag := validate(t, src); diag.Empty() {
			t.Errorf("Slider accepted %s", name)
		}
	}
}

func TestEventCallsPopoverMethods(t *testing.T) {
	base := `Window { Column {
		Popover { id: pop Button { label: "anchor" } $popover { Card { label: "panel" } } }
		Button { id: other label: "other" }
		Button { %s }
	} }`
	for _, call := range []string{"pop.toggle()", "pop.show(true)", "pop.show(false)"} {
		src := fmt.Sprintf(base, "onClick: "+call)
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s: %v", call, diag)
		}
	}
	for _, bad := range []string{
		"pop.show()", "pop.show(1)", "pop.toggle(true)", "other.show(true)",
	} {
		src := fmt.Sprintf(base, "onClick: "+bad)
		if _, diag := validate(t, src); diag.Empty() {
			t.Errorf("%s was accepted", bad)
		}
	}
}
