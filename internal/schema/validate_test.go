package schema

import (
	"strings"
	"testing"

	"github.com/ksckaan1/goui/internal/syntax"
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
		{"root not window", `Column { }`, "must be Window", ""},
		{"leaf with child", `Window { Rectangle { Text { } } }`, "cannot contain", ""},
		{"bad color literal", `Window { background: #12345 }`, "color must be", ""},
		{"bad enum type", `Window { Column { align: 3 } }`, "expects an identifier", ""},
		{"unknown breakpoint", `Window { width: { md: 1, xxl: 2 } }`, "no breakpoint", "xl"},
		{"bad breakpoint value", `Window { width: { md: "wide" } }`, "expects a number", ""},
		{"nested map", `Window { width: { md: { base: 1 } } }`, "cannot be another map", ""},
		{"responsive id", `Window { Text { id: { md: a } } }`, "cannot be responsive", ""},
		{"repeated breakpoint", `Window { width: { md: 1, md: 2 } }`, "repeats the breakpoint", ""},
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
			_, diag = Validate(f)
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
	got := diag.Format("app.ui", []byte("Window {\n  width: \"x\"\n}\n"))
	if !strings.Contains(got, "app.ui:2:10:") {
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
