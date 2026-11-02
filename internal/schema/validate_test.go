package schema

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ksckaan1/goi/internal/syntax"
)

func validate(t *testing.T, src string) (*Result, syntax.Diagnostics) {
	t.Helper()
	f, diag := syntax.Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("parse: %v", diag)
	}
	return Validate(f)
}

func TestValidFile(t *testing.T) {
	src := `Window {
		id: main
		title: "Counter"
		width: 480
		Column {
			padding: 16
			spacing: 8
			align: center
			Text { id: label text: "0" fontSize: 32 color: #222222 }
		}
	}`
	res, diag := validate(t, src)
	if !diag.Empty() {
		t.Fatalf("unexpected: %v", diag)
	}
	if len(res.IDs) != 2 {
		t.Fatalf("ids = %v", res.IDs)
	}
}

func TestStringAcceptsBareWord(t *testing.T) {
	_, diag := validate(t, `Window { title: Counter }`)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
}

func TestQuotedColor(t *testing.T) {
	_, diag := validate(t, `Window { background: "#3b82f6" }`)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	_, diag = validate(t, `Window { background: "#xyz" }`)
	if diag.Empty() {
		t.Fatal("expected a color error")
	}
}

// TestFragmentRootCanBeAnyElement is the composition rule: a fragment is a piece
// of a design, so its root is whatever element it draws. Only an entry point must
// be a Window.
func TestFragmentRootCanBeAnyElement(t *testing.T) {
	f, diag := syntax.Parse([]byte(`param title: ""
Column {
	Text { text: $title }
}`))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	// A `$param` is legal in a fragment: the default is the type witness.
	if _, diag := Validate(f); !diag.Empty() {
		t.Fatalf("fragment param rejected: %v", diag)
	}
	if _, diag := ValidateEntry(f); diag.Empty() {
		t.Fatal("an entry point cannot have unbound params")
	}

	f, diag = syntax.Parse([]byte(`Column { Text { text: "x" } }`))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if _, diag := Validate(f); !diag.Empty() {
		t.Fatalf("fragment root rejected: %v", diag)
	}
	if _, diag := ValidateEntry(f); diag.Empty() {
		t.Fatal("an entry point must be a Window")
	}
}

func TestDiagnosticCases(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
		hint string
	}{
		{"unknown element", `Window { Colum { } }`, "unknown element", "Column"},
		{"unknown property", `Window { titl: "x" }`, "unknown property", "title"},
		{"wrong type", `Window { width: "wide" }`, "expects a number", ""},
		{"wrong bool", `Window { visible: 1 }`, "expects true or false", ""},
		{"unknown enum", `Window { Column { align: centr } }`, "no value", "center"},
		{"duplicate id", `Window { Text { id: a } Text { id: a } }`, "duplicate id", ""},
		{"bad id", `Window { Text { id: A } }`, "invalid id", ""},
		{"duplicate prop", `Window { width: 1 width: 2 }`, "duplicate property", ""},

		{"leaf with child", `Window { Rectangle { Text { } } }`, "cannot contain", ""},
		{"bad color literal", `Window { background: #12345 }`, "color must be", ""},
		{"bad enum type", `Window { Column { align: 3 } }`, "expects an identifier", ""},
		{"unknown breakpoint", `Window { width: { md: 1, xxl: 2 } }`, "no breakpoint", "xl"},
		{"bad breakpoint value", `Window { width: { md: "wide" } }`, "expects a number", ""},
		{"nested map", `Window { width: { md: { base: 1 } } }`, "cannot be another map", ""},
		{"responsive id", `Window { Text { id: { md: a } } }`, "cannot be responsive", ""},
		{"repeated breakpoint", `Window { width: { md: 1, md: 2 } }`, "repeats the breakpoint", ""},
		{"positioned outside stack", `Window { Column { Positioned { left: 1 } } }`, "direct child of Stack", ""},
		{"percent on padding", `Window { padding: 50% }`, "only supported for width and height", ""},
		{"unknown ref id", `Window { Text { id: a width: b.width } }`, "unknown id", ""},
		{"unknown ref prop", `Window { Text { id: a width: a.nope } }`, "has no property", ""},
		{"ref not a number", `Window { Text { id: a text: "x" } Text { width: a.text } }`, "not number", ""},
		{"ref to percent", `Window { Text { id: a width: 50% } Text { width: a.width } }`, "cannot be referenced", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, diag := syntax.Parse([]byte(tc.src))
			if f != nil && !diag.Empty() {
				// Parser-level rejection is also acceptable for malformed literals.
				if !strings.Contains(diag.Error(), tc.want) {
					t.Fatalf("parse diag %q missing %q", diag.Error(), tc.want)
				}
				return
			}
			_, diag = ValidateEntry(f)
			if diag.Empty() {
				t.Fatalf("expected a diagnostic")
			}
			msg := diag.Error()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("diagnostics %q missing %q", msg, tc.want)
			}
			if tc.hint != "" && !strings.Contains(msg, tc.hint) {
				t.Fatalf("diagnostics %q missing hint %q", msg, tc.hint)
			}
		})
	}
}

func TestPositionedInStack(t *testing.T) {
	_, diag := validate(t, `Window {
		Stack {
			Positioned { left: 8 top: 8 width: 40 height: 20 }
			Positioned { right: 0 bottom: 0 Text { text: "hi" } }
		}
	}`)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
}

func TestResponsiveValues(t *testing.T) {
	_, diag := validate(t, `Window {
		width: { base: 320, md: 480, lg: 640 }
		background: { md: #123456 }
		Column { padding: { sm: 8 } align: { lg: center } }
	}`)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
}

func TestExpressionValues(t *testing.T) {
	_, diag := validate(t, `Window {
		width: 400
		Column {
			Rectangle { id: a width: 120 height: 40 }
			Rectangle { id: b width: a.width / 2 height: 25% }
			Rectangle { id: c width: (a.width + b.width) * 0.5 - 4 }
			Rectangle { id: d width: { base: 50%, md: a.width - 8 } }
			Rectangle { id: e width: a.width-8 }
		}
	}`)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
}

// TestColorRoles checks the role names a .goi file can use for a color, and that
// a typo points at the nearest role instead of failing at paint time.
func TestColorRoles(t *testing.T) {
	_, diag := validate(t, `Window {
		background: surface
		Column {
			Rectangle { id: a background: surfaceContainerHigh borderColor: outlineVariant }
			Text { text: "hi" color: onSurfaceVariant }
			Text { text: "typo" color: surfaceContanerHigh }
		}
	}`)
	if len(diag) != 1 {
		t.Fatalf("want one diagnostic, got %v", diag)
	}
	if diag[0].Hint == "" || !strings.Contains(diag[0].Hint, "surfaceContainerHigh") {
		t.Fatalf("hint = %q, want the nearest role", diag[0].Hint)
	}
}

func TestVisibleOnComponents(t *testing.T) {
	_, diag := validate(t, `Window {
		Column {
			Button { label: "hidden" visible: false }
			Card { label: "responsive" visible: { base: true, md: false } }
			IconButton { icon: home visible: true }
		}
	}`)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
}

// TestEveryPropertyAcceptsPlainAndResponsive pins the rule that a property
// which takes a breakpoint map always takes its plain literal too.
func TestEveryPropertyAcceptsPlainAndResponsive(t *testing.T) {
	for _, name := range ElementNames() {
		def, _ := Lookup(name)
		for _, pd := range def.Attrs {
			if pd.Type == TypeID {
				continue
			}
			val := plainValue(pd)
			v := &validator{
				ids:   map[*syntax.Element]string{},
				elems: map[string]*syntax.Element{},
				seen:  map[string]syntax.Pos{},
			}
			e := &syntax.Element{Name: name}
			v.checkLiteral(e, &pd, val)
			v.checkMap(e, &pd, &syntax.Map{Entries: []syntax.MapEntry{{Key: "md", Value: val}}})
			if !v.diag.Empty() {
				t.Errorf("%s.%s rejects a plain or responsive value: %v", name, pd.Name, v.diag)
			}
		}
	}
}

func plainValue(pd Prop) syntax.Value {
	switch pd.Type {
	case TypeString:
		return &syntax.String{Val: "x"}
	case TypeNumber:
		return &syntax.Number{Val: 1}
	case TypeBool:
		return &syntax.Bool{Val: true}
	case TypeColor:
		return &syntax.Color{RGBA: 0x112233FF}
	case TypeEnum:
		if len(pd.Enum) > 0 {
			return &syntax.Ident{Val: pd.Enum[0]}
		}
	}
	return &syntax.Ident{Val: "start"}
}

func TestAllDiagnosticsReported(t *testing.T) {
	src := `Window {
		width: "a"
		height: "b"
		Colum { }
	}`
	f, _ := syntax.Parse([]byte(src))
	_, diag := Validate(f)
	if len(diag) < 3 {
		t.Fatalf("expected 3+ diagnostics, got %d: %v", len(diag), diag)
	}
}

func TestDiagnosticHasPosition(t *testing.T) {
	f, _ := syntax.Parse([]byte("Window {\n  width: \"x\"\n}\n"))
	_, diag := Validate(f)
	if len(diag) != 1 {
		t.Fatalf("got %d", len(diag))
	}
	got := diag.Format("app.goi", []byte("Window {\n  width: \"x\"\n}\n"))
	if !strings.Contains(got, "app.goi:2:10:") {
		t.Fatalf("missing position: %s", got)
	}
	if !strings.Contains(got, "width: \"x\"") || !strings.Contains(got, "^") {
		t.Fatalf("missing snippet: %s", got)
	}
}

func TestSchemaDefaultsAreTyped(t *testing.T) {
	win, ok := Lookup("Window")
	if !ok {
		t.Fatal("no Window")
	}
	for _, p := range win.Attrs {
		switch p.Type {
		case TypeNumber:
			if _, ok := p.Default.(float64); !ok {
				t.Errorf("%s default is %T, want float64", p.Name, p.Default)
			}
		case TypeBool:
			if _, ok := p.Default.(bool); !ok {
				t.Errorf("%s default is %T, want bool", p.Name, p.Default)
			}
		case TypeColor:
			if _, ok := p.Default.(uint32); !ok {
				t.Errorf("%s default is %T, want uint32", p.Name, p.Default)
			}
		case TypeString:
			if _, ok := p.Default.(string); !ok {
				t.Errorf("%s default is %T, want string", p.Name, p.Default)
			}
		}
	}
}

func TestCommonPropsOnEveryElement(t *testing.T) {
	for _, name := range ElementNames() {
		e, _ := Lookup(name)
		for _, want := range []string{"id", "width", "padding", "background", "visible"} {
			if e.Prop(want) == nil {
				t.Errorf("%s is missing common property %s", name, want)
			}
		}
	}
}

// TestIfBlockValidation keeps the block honest: a condition must be a bool, a
// leaf cannot hold one, and an arm holds elements, not properties.
func TestIfBlockValidation(t *testing.T) {
	bad := map[string]string{
		"Window {\n\tColumn {\n\t\t{#if 5}\n\t\t\tText { text: \"a\" }\n\t\t{/if}\n\t}\n}":                                              "must be a bool",
		"Window {\n\tColumn {\n\t\t{#if \"x\"}\n\t\t\tText { text: \"a\" }\n\t\t{/if}\n\t}\n}":                                          "must be a bool",
		"Window {\n\tColumn {\n\t\t{#if loading}\n\t\t\tText { text: \"a\" }\n\t\t{/if}\n\t}\n}":                                        "must be a bool",
		"Window {\n\tText {\n\t\t{#if app.dark}\n\t\t\tText { text: \"a\" }\n\t\t{/if}\n\t}\n}":                                         "cannot contain child elements",
		"Window {\n\tColumn {\n\t\t{#if app.dark}\n\t\t\ttext: \"a\"\n\t\t{/if}\n\t}\n}":                                                "cannot declare the property",
		"Window {\n\tColumn {\n\t\t{#if box.checked}\n\t\t\tText { text: \"a\" }\n\t\t{/if}\n\t}\n}":                                    "references unknown id",
		"Window {\n\tColumn {\n\t\tText { id: label text: \"x\" }\n\t\t{#if label.text}\n\t\t\tText { text: \"a\" }\n\t\t{/if}\n\t}\n}": "is string, not bool",
	}
	for src, want := range bad {
		_, diag := validate(t, src)
		if diag.Empty() {
			t.Errorf("%q was accepted", src)
			continue
		}
		if got := diag.Format("<src>", []byte(src)); !strings.Contains(got, want) {
			t.Errorf("%q gave %s, want %q", src, got, want)
		}
	}

	// The good cases: a reference to a bool property, a comparison, app.dark.
	good := []string{
		"Window {\n\tColumn {\n\t\t{#if app.dark}\n\t\t\tText { text: \"a\" }\n\t\t{/if}\n\t}\n}",
		"Window {\n\tColumn {\n\t\t{#if window.width > 900}\n\t\t\tText { text: \"a\" }\n\t\t{/if}\n\t}\n}",
		"Window {\n\tColumn {\n\t\tCheckbox { id: box checked: true }\n\t\t{#if box.checked}\n\t\t\tText { text: \"a\" }\n\t\t{/if}\n\t}\n}",
		"Window {\n\tColumn {\n\t\t{#if app.dark}\n\t\t\tText { text: \"a\" }\n\t\t{:else}\n\t\t\tRow { }\n\t\t{/if}\n\t}\n}",
	}
	for _, src := range good {
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s: %v", src, diag.Format("<src>", []byte(src)))
		}
	}
}

// TestIfBlockSampleIsTheShippedDocs guard: the example in docs/SYNTAX.md must
// stay valid, because it is what a reader copies.
func TestIfBlockSampleIsTheShippedDocs(t *testing.T) {
	body, err := os.ReadFile("../../docs/SYNTAX.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range regexp.MustCompile("(?s)```goi\n(.*?)```").FindAllStringSubmatch(string(body), -1) {
		src := block[1]
		if !strings.Contains(src, "{#if") {
			continue
		}
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("docs/SYNTAX.md block:\n%s\n%v", src, diag.Format("doc", []byte(src)))
		}
	}
}
