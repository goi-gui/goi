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
        TextField { id: name  placeholder: "Your name"  width: 240 }
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

func TestParseExpressions(t *testing.T) {
	src := `A {
		w: 50%
		x: box.width / 2
		y: (a.width + 16) * 2 - 4
		z: -1
		n: a.width-8
	}`
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	want := `A
  w = 50%
  x = (box.width / 2)
  y = (((a.width + 16) * 2) - 4)
  z = -1
  n = (a.width - 8)
`
	if got := Dump(f); got != want {
		t.Fatalf("dump mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestParseExpressionDiagnostics(t *testing.T) {
	for _, src := range []string{
		"A { w: box.width / }",
		"A { w: (1 + 2 }",
		"A { w: box. }",
	} {
		if _, diag := Parse([]byte(src)); diag.Empty() {
			t.Errorf("%s: expected a diagnostic", src)
		}
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
		{"lone minus", "A { n: - }", "expected a value"},
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
	got := diag.Format("x.goi", nil)
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

func TestParsePreamble(t *testing.T) {
	f, diag := Parse([]byte(`import Sidebar "./Sidebar.goi"
import Card "parts/Card.goi"
props {
	Title  string
	Accent color
	Count  int
}

Column { Text { text: props.Title } }`))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if len(f.Imports) != 2 {
		t.Fatalf("imports = %d, want 2", len(f.Imports))
	}
	if f.Imports[0].Path != "./Sidebar.goi" || f.Imports[0].Alias != "Sidebar" {
		t.Fatalf("first import = %+v", f.Imports[0])
	}
	if len(f.Props) != 3 {
		t.Fatalf("props = %d, want 3", len(f.Props))
	}
	if f.Props[0].Name != "Title" || f.Props[0].Type.String() != "string" {
		t.Fatalf("first prop = %+v", f.Props[0])
	}
	if f.Props[2].Type.String() != "int" {
		t.Fatalf("third prop = %+v", f.Props[2])
	}
	// A fragment's root need not be a Window.
	if f.Root.Name != "Column" {
		t.Fatalf("root = %s", f.Root.Name)
	}
	// `props.Title` is an ordinary reference; asset expansion substitutes it.
	ref, ok := f.Root.Children()[0].Prop("text").Value.(*Ref)
	if !ok || ref.ID != "props" || ref.Prop != "Title" {
		t.Fatalf("value = %#v, want a props reference", f.Root.Children()[0].Prop("text").Value)
	}
}

func TestParsePreambleErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"path first", `import "./a.goi" as A
Window { }`, "expected an alias after import"},
		{"missing path", `import A
Window { }`, "expected a quoted path"},
		{"missing brace", "props Title string\nWindow { }", "expected '{' after props"},
		{"missing type", "props { Title }\nWindow { }", "expected a type"},
		{"bad slice", "props { Rows [] }\nWindow { }", "expected a type"},
		{"dollar in a value", "Window { Text { text: $ } }", "unexpected '$' in a value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diag := Parse([]byte(tc.src))
			if diag.Empty() {
				t.Fatalf("expected a diagnostic")
			}
			if !strings.Contains(diag[0].Message, tc.want) {
				t.Fatalf("message = %q, want %q", diag[0].Message, tc.want)
			}
		})
	}
}

func TestParseDumpKeepsPreamble(t *testing.T) {
	f, diag := Parse([]byte(`import S "./S.goi"
props {
	N     int
	Label string
}
bridge { Rows []S }
type S { Name string }
Column { }`))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	got := Dump(f)
	for _, want := range []string{`import S "./S.goi"`, "props N int", "props Label string", "bridge Rows []S", "type S", "  Name string"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dump is missing %q:\n%s", want, got)
		}
	}
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

func TestParseChildrenSlot(t *testing.T) {
	f, diag := Parse([]byte("Column {\n\tgap: 5\n\t$children\n}\n"))
	if !diag.Empty() {
		t.Fatalf("unexpected diagnostics: %v", diag)
	}
	if !f.IsFragment() {
		t.Fatal("a file with $children is not a fragment")
	}
	kids := f.Root.Children()
	if len(kids) != 1 || kids[0].Name != "$children" {
		t.Fatalf("children = %v", kids)
	}

	// Any name is a slot; a bare one marks the file as a fragment.
	f, diag = Parse([]byte("Column {\n\t$header\n}\n"))
	if !diag.Empty() {
		t.Fatalf("$header was rejected as a slot: %v", diag)
	}
	if !f.IsFragment() || f.Root.Children()[0].Name != "$header" {
		t.Fatalf("$header = %v, fragment = %v", f.Root.Children(), f.Fragment)
	}

	// With a body it is how a caller passes elements, so the file is not a
	// fragment by itself.
	f, diag = Parse([]byte("Card {\n\t$header {\n\t\tText { text: \"x\" }\n\t}\n}\n"))
	if !diag.Empty() {
		t.Fatalf("a named slot body was rejected: %v", diag)
	}
	if f.IsFragment() {
		t.Fatal("a call that fills a slot makes the caller a fragment")
	}
	slot := f.Root.Children()[0]
	if slot.Name != "$header" || len(slot.Children()) != 1 {
		t.Fatalf("slot = %+v", slot)
	}
}

func TestParseIfBlock(t *testing.T) {
	src := `Window {
	Column {
		if app.dark {
			Text { text: "a" }
		} else if window.width > 900 {
			Text { text: "b" }
		} else {
			Text { text: "c" }
		}
	}
}`
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("unexpected diagnostics: %v", diag)
	}
	blk, ok := f.Root.Children()[0].Members[0].(*Block)
	if !ok {
		t.Fatalf("first member is %T, want a block", f.Root.Children()[0].Members[0])
	}
	if blk.Kind != BlockIf || len(blk.Branches) != 3 {
		t.Fatalf("branches = %+v", blk.Branches)
	}
	if blk.Branches[0].Cond == nil || blk.Branches[1].Cond == nil {
		t.Fatal("if/else-if lost their conditions")
	}
	if blk.Branches[2].Cond != nil {
		t.Fatal("the else arm has a condition")
	}
	if len(blk.Branches[0].Members) != 1 || blk.Branches[0].Members[0].(*Element).Name != "Text" {
		t.Fatalf("arm body = %v", blk.Branches[0].Members)
	}
	if blk.Close == 0 {
		t.Fatal("block has no closing position")
	}
}

func TestParseNestedIfBlocks(t *testing.T) {
	src := `Window {
	if app.dark {
		if window.width > 900 {
			Text { text: "a" }
		}
	}
}`
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("unexpected diagnostics: %v", diag)
	}
	outer := f.Root.Members[0].(*Block)
	inner, ok := outer.Branches[0].Members[0].(*Block)
	if !ok {
		t.Fatalf("nested member is %T, want a block", outer.Branches[0].Members[0])
	}
	if inner.Close == 0 || inner.Branches[0].Cond == nil {
		t.Fatal("the inner block was not parsed")
	}
}

func TestParseIfBlockErrors(t *testing.T) {
	cases := map[string]string{
		"if {\n\t\t\tText { text: \"a\" }\n\t\t}":           "expected '{' to open the block body",
		"{#if app.dark}\n\t\tText { text: \"a\" }\n\t{/if}": "the {#if}/{#each} form is gone",
		"for x {\n\t\t}": "expected ':=' after the loop variable",
	}
	for body, want := range cases {
		src := "Window {\n\tColumn {\n\t\t" + body + "\n\t}\n}\n"
		_, diag := Parse([]byte(src))
		if diag.Empty() {
			t.Errorf("%q was accepted", body)
			continue
		}
		if !strings.Contains(diag.Format("<src>", []byte(src)), want) {
			t.Errorf("%q gave %v, want %q", body, diag.Format("<src>", []byte(src)), want)
		}
	}
}

func TestParseDataAndFor(t *testing.T) {
	src := `bridge {
	loading bool
	tickets []ticket
}
type ticket {
	title  string
	unread bool
}
Window {
	for i, t := range bridge.tickets {
		Text { text: t.title }
	}
}`
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("unexpected diagnostics: %v", diag)
	}
	if len(f.Vars) != 2 || f.Vars[1].Name != "tickets" || f.Vars[1].Type.Kind != TypeSlice || f.Vars[1].Type.Elem.Name != "ticket" {
		t.Fatalf("vars = %+v", f.Vars)
	}
	if len(f.Shapes) != 1 || f.Shapes[0].Name != "ticket" || len(f.Shapes[0].Fields) != 2 {
		t.Fatalf("shapes = %+v", f.Shapes)
	}
	blk, ok := f.Root.Members[0].(*Block)
	if !ok || blk.Kind != BlockFor || blk.KeyVar != "i" || blk.ValVar != "t" {
		t.Fatalf("block = %+v", f.Root.Members[0])
	}
	if ref, ok := blk.Range.(*Ref); !ok || ref.ID != "bridge" || ref.Prop != "tickets" {
		t.Fatalf("range = %#v", blk.Range)
	}
}

func TestParseForForms(t *testing.T) {
	cases := []struct {
		src        string
		key, val   string
		wantErrSub string
	}{
		{"for _, row := range bridge.rows { }", "_", "row", ""},
		{"for row := range bridge.rows { }", "row", "", ""},
		{"for range bridge.rows { }", "", "", ""},
		{"for i := range 5 { }", "i", "", ""},
		{"for i, row := range bridge.rows { }", "i", "row", ""},
	}
	for _, tc := range cases {
		src := "bridge { rows []row }\ntype row { a string }\nWindow { Column { " + tc.src + " } }"
		f, diag := Parse([]byte(src))
		if !diag.Empty() {
			t.Errorf("%q: %v", tc.src, diag)
			continue
		}
		blk := f.Root.Children()[0].Members[0].(*Block)
		if blk.KeyVar != tc.key || blk.ValVar != tc.val {
			t.Errorf("%q: key=%q val=%q, want %q/%q", tc.src, blk.KeyVar, blk.ValVar, tc.key, tc.val)
		}
	}
}

func TestParseSwitchBlock(t *testing.T) {
	src := `bridge { job string }
Window {
	Column {
		switch bridge.job {
		case "swe", "eee", "fe":
			Text { text: "engineer" }
			Text { text: "builds" }
		case "teacher":
			Text { text: "teaches" }
		default:
			Text { text: "other" }
		}
	}
}`
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("unexpected diagnostics: %v", diag)
	}
	blk, ok := f.Root.Children()[0].Members[0].(*Block)
	if !ok || blk.Kind != BlockSwitch {
		t.Fatalf("block = %+v", f.Root.Children()[0].Members[0])
	}
	if ref, ok := blk.Subject.(*Ref); !ok || ref.ID != "bridge" || ref.Prop != "job" {
		t.Fatalf("subject = %#v", blk.Subject)
	}
	if len(blk.Branches) != 3 {
		t.Fatalf("branches = %d", len(blk.Branches))
	}
	if first := blk.Branches[0]; len(first.Vals) != 3 || len(first.Members) != 2 {
		t.Fatalf("first arm = %+v", first)
	}
	if def := blk.Branches[2]; def.Vals != nil || len(def.Members) != 1 {
		t.Fatalf("default arm = %+v", def)
	}
}

func TestParseSwitchErrors(t *testing.T) {
	cases := map[string]string{
		`Window { Column { switch bridge.job case "x": } }`:    "expected '{' to open the switch body",
		`Window { Column { switch bridge.job { case "x" } } }`: "expected ':' after a case value",
		`Window { Column { switch bridge.job { default } } }`:  "expected ':' after default",
		`Window { Column { switch bridge.job { "x": } } }`:     "expected case, default or '}'",
	}
	for src, want := range cases {
		_, diag := Parse([]byte(src))
		if diag.Empty() {
			t.Errorf("%q was accepted", src)
			continue
		}
		if got := diag.Format("<src>", []byte(src)); !strings.Contains(got, want) {
			t.Errorf("%q gave %s, want %q", src, got, want)
		}
	}
}

func TestParseDataErrors(t *testing.T) {
	cases := map[string]string{
		"data { rows [] }\nWindow { }":            "expected a type",
		"data { rows: []row }\nWindow { }":        "expected a type",
		"data rows\nWindow { }":                   "expected '{' after the shape name",
		"bridge { r map[int]string }\nWindow { }": "",
	}
	for src, want := range cases {
		_, diag := Parse([]byte(src))
		if want == "" {
			if !diag.Empty() {
				t.Errorf("%q was rejected: %v", src, diag)
			}
			continue
		}
		if diag.Empty() {
			t.Errorf("%q was accepted", src)
			continue
		}
		if got := diag.Format("<src>", []byte(src)); !strings.Contains(got, want) {
			t.Errorf("%q gave %s, want %q", src, got, want)
		}
	}
}

// TestParseStringConcatenation keeps the documented `+` promise: two strings,
// or a string and a reference, concatenate.
func TestParseStringConcatenation(t *testing.T) {
	src := `Window {
	Column {
		Text { text: "Conditional: the " + app.mode + " scheme is active." }
	}
}`
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("unexpected diagnostics: %v", diag)
	}
	prop := f.Root.Children()[0].Children()[0].Prop("text")
	if _, ok := prop.Value.(*Binary); !ok {
		t.Fatalf("value = %T, want a binary expression", prop.Value)
	}
}

func TestParseBridge(t *testing.T) {
	src := `bridge {
	Posts  []post
	OnOpen func(id string)
	OnSave func(title string, count uint)
}
type post { id string, title string }
Window {
	Column {
		for _, post := range bridge.Posts {
			Button { label: post.title onClick: bridge.OnOpen(post.id) }
		}
		Slider { onChange: bridge.OnSave("x", event) }
	}
}`
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("unexpected diagnostics: %v", diag)
	}
	if len(f.Vars) != 3 {
		t.Fatalf("vars = %+v", f.Vars)
	}
	posts := f.Vars[0]
	if posts.Type.Kind != TypeSlice || posts.Type.Elem.Name != "post" {
		t.Fatalf("posts = %+v", posts)
	}
	open := f.Vars[1]
	if open.Type.Kind != TypeFunc || len(open.Type.Args) != 1 || open.Type.Args[0].Name != "id" || open.Type.Args[0].Type.Name != "string" {
		t.Fatalf("OnOpen = %+v", open)
	}
	save := f.Vars[2]
	if len(save.Type.Args) != 2 || save.Type.Args[1].Name != "count" || save.Type.Args[1].Type.Name != "uint" {
		t.Fatalf("OnSave = %+v", save)
	}
	if len(f.Shapes) != 1 || f.Shapes[0].Name != "post" || len(f.Shapes[0].Fields) != 2 {
		t.Fatalf("shapes = %+v", f.Shapes)
	}
	loop := f.Root.Members[0].(*Element).Members[0].(*Block)
	if loop.Kind != BlockFor || loop.ValVar != "post" {
		t.Fatalf("loop = %+v", loop)
	}
	btn := loop.Branches[0].Members[0].(*Element)
	call, ok := btn.Prop("onClick").Value.(*Call)
	if !ok || call.Callee.ID != "bridge" || call.Callee.Prop != "OnOpen" || len(call.Args) != 1 {
		t.Fatalf("onClick = %#v", btn.Prop("onClick").Value)
	}
}

// TestParseFormatf keeps the builtin call a value, not an event-only call.
func TestParseFormatf(t *testing.T) {
	src := "Window { Column { Text { text: formatf(\"Count is %d\", bridge.Count) } } }"
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	call, ok := f.Root.Children()[0].Children()[0].Prop("text").Value.(*Call)
	if !ok || call.Name != "formatf" || call.Callee != nil || len(call.Args) != 2 {
		t.Fatalf("value = %#v", f.Root.Children()[0].Children()[0].Prop("text").Value)
	}
}

func TestParseBridgeErrors(t *testing.T) {
	cases := map[string]string{
		"bridge { OnOpen func }\nWindow { }":         "expected '(' after func",
		"bridge { OnOpen func(id) }\nWindow { }":     "expected a type",
		"bridge { Rows [] }\nWindow { }":             "expected a type",
		"bridge { Rows map[int]string }\nWindow { }": "",
		"bridge { N int = 0 }\nWindow { }":           "expected a variable name",
	}
	for src, want := range cases {
		_, diag := Parse([]byte(src))
		if want == "" {
			if !diag.Empty() {
				t.Errorf("%q was rejected: %v", src, diag)
			}
			continue
		}
		if diag.Empty() {
			t.Errorf("%q was accepted", src)
			continue
		}
		if got := diag.Format("<src>", []byte(src)); !strings.Contains(got, want) {
			t.Errorf("%q gave %s, want %q", src, got, want)
		}
	}
}
