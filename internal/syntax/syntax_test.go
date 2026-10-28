package syntax

import (
	"strings"
	"testing"
)

const validUI = `// a counter
Window {
    id: main
    title: "Counter"   // trailing comment
    width: 480
    height: 320.5

    /* block
       comment */
    Column {
        padding: 16
        spacing: 8

        Text   { id: label  text: "0"  fontSize: 32  color: #222222 }
        Button { id: inc    text: "Increase"  background: #3b82f6  color: #ffffff }
        TextInput { id: name  placeholder: "Your name"  width: 240 }
    }
}
`

func TestParseValid(t *testing.T) {
	f, diag := Parse([]byte(validUI))
	if !diag.Empty() {
		t.Fatalf("unexpected diagnostics: %v", diag)
	}
	if f.Root.Name != "Window" {
		t.Fatalf("root = %q", f.Root.Name)
	}
	if got := f.Root.Prop("title").Value.(*String).Val; got != "Counter" {
		t.Fatalf("title = %q", got)
	}
	if got := f.Root.Prop("height").Value.(*Number).Val; got != 320.5 {
		t.Fatalf("height = %v", got)
	}
	children := f.Root.Children()
	if len(children) != 1 || children[0].Name != "Column" {
		t.Fatalf("children = %v", children)
	}
	col := children[0]
	if len(col.Children()) != 3 {
		t.Fatalf("column children = %d", len(col.Children()))
	}
	text := col.Children()[0]
	if got := text.Prop("color").Value.(*Color).RGBA; got != 0x222222FF {
		t.Fatalf("color = %08x", got)
	}
}

func TestParseColors(t *testing.T) {
	cases := map[string]uint32{
		"#fff":      0xFFFFFFFF,
		"#f00":      0xFF0000FF,
		"#3b82f6":   0x3B82F6FF,
		"#12345678": 0x12345678,
	}
	for src, want := range cases {
		f, diag := Parse([]byte("A { c: " + src + " }"))
		if !diag.Empty() {
			t.Fatalf("%s: %v", src, diag)
		}
		got := f.Root.Prop("c").Value.(*Color).RGBA
		if got != want {
			t.Errorf("%s = %08x, want %08x", src, got, want)
		}
	}
	// A quoted "#rrggbb" stays a String here; the schema coerces it so that a
	// string literal is never silently reinterpreted by the lexer.
	f, diag := Parse([]byte(`A { c: "#3b82f6" }`))
	if !diag.Empty() {
		t.Fatalf("quoted color: %v", diag)
	}
	if got := f.Root.Prop("c").Value.(*String).Val; got != "#3b82f6" {
		t.Errorf("quoted color = %q", got)
	}
}

func TestParseStringEscapes(t *testing.T) {
	src := `A { s: "q\" b\\ t\t nl\n u\u00e7 emoji\uD83D\uDE00" }`
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	want := "q\" b\\ t\t nl\n uç emoji😀"
	if got := f.Root.Prop("s").Value.(*String).Val; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseBooleansAndNegative(t *testing.T) {
	f, diag := Parse([]byte("A { a: true b: false c: -12.25 }"))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if !f.Root.Prop("a").Value.(*Bool).Val {
		t.Error("a should be true")
	}
	if f.Root.Prop("b").Value.(*Bool).Val {
		t.Error("b should be false")
	}
	if got := f.Root.Prop("c").Value.(*Number).Val; got != -12.25 {
		t.Errorf("c = %v", got)
	}
}

func TestParseSemicolons(t *testing.T) {
	f, diag := Parse([]byte("A { a: 1; b: 2; }"))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if len(f.Root.Props()) != 2 {
		t.Fatalf("props = %d", len(f.Root.Props()))
	}
}

func TestParseEnumIdent(t *testing.T) {
	f, diag := Parse([]byte("A { align: center }"))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if got := f.Root.Prop("align").Value.(*Ident).Val; got != "center" {
		t.Fatalf("align = %q", got)
	}
}

func TestDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // substring expected in the rendered diagnostics
	}{
		{"missing brace", "Window {", "missing '}'"},
		{"unterminated string", `A { s: "abc }`, "unterminated string"},
		{"unterminated comment", "A { /* x }", "unterminated block comment"},
		{"bad color", "A { c: #12345 }", "color must be"},
		{"bad escape", `A { s: "\q" }`, "invalid escape"},
		{"missing value", "A { s: }", "expected a value"},
		{"missing colon", "A { s \"x\" }", "expected"},
		{"two roots", "A { } B { }", "after the root element"},
		{"lone minus", "A { n: - }", "expected a digit"},
		{"no root", "", "expected an element"},
		{"junk char", "A { s: 1 @ }", "unexpected character"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diag := Parse([]byte(tc.src))
			if diag.Empty() {
				t.Fatalf("expected a diagnostic")
			}
			msg := diag.Error()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("diagnostics %q do not contain %q", msg, tc.want)
			}
		})
	}
}

func TestDiagnosticPositions(t *testing.T) {
	src := "Window {\n  width: \"x\"\n}\n"
	f, diag := Parse([]byte(src))
	if f == nil || !diag.Empty() {
		t.Fatalf("parse should succeed, diag=%v", diag)
	}
	// Position of the value literal: line 2, col 10.
	at := f.Root.Prop("width").Value.Pos()
	got := diag.Format("x.ui", nil)
	_ = got
	lines := lineStarts([]byte(src))
	line, col := lines.locate(at)
	if line != 2 || col != 10 {
		t.Fatalf("pos = %d:%d, want 2:10", line, col)
	}
}

func TestMultipleDiagnostics(t *testing.T) {
	src := "A {\n  s: \"unterminated\n  c: #zzz\n}\n"
	_, diag := Parse([]byte(src))
	if len(diag) < 2 {
		t.Fatalf("expected at least 2 diagnostics, got %d: %v", len(diag), diag)
	}
}

func TestErrorRecoveryKeepsLaterMembers(t *testing.T) {
	src := "A {\n  a: 1\n  b: @@@\n  c: 3\n}\n"
	f, diag := Parse([]byte(src))
	if diag.Empty() {
		t.Fatal("expected a diagnostic")
	}
	if f == nil {
		t.Fatal("expected a partial AST")
	}
	if f.Root.Prop("c") == nil {
		t.Fatal("recovery lost the property after the error")
	}
}

func TestSuggest(t *testing.T) {
	if got := Suggest("colr", []string{"color", "width"}); got != "color" {
		t.Errorf("suggest = %q", got)
	}
	if got := Suggest("zzzzzz", []string{"color"}); got != "" {
		t.Errorf("suggest unrelated = %q", got)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(validUI))
	f.Add([]byte("A { s: \"\\u00e7\" }"))
	f.Add([]byte("A {"))
	f.Add([]byte("A { c: #123 }"))
	f.Fuzz(func(t *testing.T, src []byte) {
		file, diag := Parse(src)
		if file == nil && diag.Empty() {
			t.Fatal("nil file with no diagnostics")
		}
		if file != nil {
			// Rendering diagnostics must not panic on any AST.
			_ = diag.Format("f.ui", src)
		}
	})
}

func TestParseResponsiveMap(t *testing.T) {
	f, diag := Parse([]byte(`Window {
		width: { base: 320, md: 480, lg: 640 }
		background: { md: #123456 }
	}`))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	m, ok := f.Root.Prop("width").Value.(*Map)
	if !ok {
		t.Fatalf("width = %T, want *Map", f.Root.Prop("width").Value)
	}
	if len(m.Entries) != 3 {
		t.Fatalf("entries = %d", len(m.Entries))
	}
	if m.Entries[0].Key != "base" || m.Entries[0].Value.(*Number).Val != 320 {
		t.Fatalf("first entry = %+v", m.Entries[0])
	}
	if m.Entries[2].Key != "lg" || m.Entries[2].Value.(*Number).Val != 640 {
		t.Fatalf("last entry = %+v", m.Entries[2])
	}
	bg, ok := f.Root.Prop("background").Value.(*Map)
	if !ok || bg.Entries[0].Value.(*Color).RGBA != 0x123456FF {
		t.Fatalf("background = %#v", f.Root.Prop("background").Value)
	}
	if got := Dump(f); !strings.Contains(got, "width = { base: 320, md: 480, lg: 640 }") {
		t.Fatalf("dump:\n%s", got)
	}
}

func TestParseMapTrailingCommaAndSpaces(t *testing.T) {
	f, diag := Parse([]byte(`Window {
		width: {
			base: 1,
			md: 2,
		}
		height: {}
	}`))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if m := f.Root.Prop("width").Value.(*Map); len(m.Entries) != 2 {
		t.Fatalf("entries = %d", len(m.Entries))
	}
	if m := f.Root.Prop("height").Value.(*Map); len(m.Entries) != 0 {
		t.Fatalf("height entries = %d", len(m.Entries))
	}
}
