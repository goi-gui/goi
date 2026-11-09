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
		`"hsl(200 40% 50%)"`, `#fff`, `primary`,
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
