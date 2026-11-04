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
	f, diag := Parse([]byte(`import "./Sidebar.goi" as Sidebar
import "parts/Card.goi" as Card
param title: "Mail"
param accent: #123456

Column { Text { text: $title } }`))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if len(f.Imports) != 2 {
		t.Fatalf("imports = %d, want 2", len(f.Imports))
	}
	if f.Imports[0].Path != "./Sidebar.goi" || f.Imports[0].Alias != "Sidebar" {
		t.Fatalf("first import = %+v", f.Imports[0])
	}
	if len(f.Params) != 2 {
		t.Fatalf("params = %d, want 2", len(f.Params))
	}
	if f.Params[0].Name != "title" {
		t.Fatalf("first param = %+v", f.Params[0])
	}
	if s, ok := f.Params[0].Default.(*String); !ok || s.Val != "Mail" {
		t.Fatalf("param default = %#v", f.Params[0].Default)
	}
	// A fragment's root need not be a Window.
	if f.Root.Name != "Column" {
		t.Fatalf("root = %s", f.Root.Name)
	}
	// $title survives parsing; asset expansion is what replaces it.
	ref, ok := f.Root.Children()[0].Prop("text").Value.(*ParamRef)
	if !ok || ref.Name != "title" {
		t.Fatalf("value = %#v, want a ParamRef", f.Root.Children()[0].Prop("text").Value)
	}
}

func TestParsePreambleErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"missing as", `import "./a.goi"
Window { }`, `expected "as"`},
		{"missing alias", `import "./a.goi" as
Window { }`, "expected an alias"},
		{"missing param name", "param : 1\nWindow { }", "expected a param name"},
		{"missing colon", "param x 1\nWindow { }\n", "expected ':'"},
		{"dollar without name", "Window { Text { text: $ } }", "expected a param name"},
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
	f, diag := Parse([]byte(`import "./S.goi" as S
param n: 4
Column { }`))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	got := Dump(f)
	if !strings.Contains(got, `import "./S.goi" as S`) || !strings.Contains(got, "param n = 4") {
		t.Fatalf("dump:\n%s", got)
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
			_ = diag.Format("f.goi", src)
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
		{#if app.dark}
			Text { text: "a" }
		{:else if window.width > 900}
			Text { text: "b" }
		{:else}
			Text { text: "c" }
		{/if}
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
	if len(blk.Branches) != 3 {
		t.Fatalf("%d branches, want 3", len(blk.Branches))
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
	{#if app.dark}
		{#if window.width > 900}
			Text { text: "a" }
		{/if}
	{/if}
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
		"{#if app.dark}\n\t\tText { text: \"a\" }\n\t}": "expected {:else} or {/if}",
		"{:else}": "expected '#'",
		"{#each rows as r}\n\t\tText { text: \"a\" }\n\t{/each}": "expected '.' after the bridge namespace",
		"{#when app.dark}\n\t{/when}":                            "only {#if} exists",
		"{#if}\n\t{/if}":                                         "expected a value",
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

func TestParseDataAndEach(t *testing.T) {
	src := `data {
	loading: bool
	tickets: ticket[]
}
data ticket {
	title: string
	unread: bool
}
Window {
	{#each data.tickets as t}
		Text { text: t.title }
	{:else}
		Text { text: "none" }
	{/each}
}`
	f, diag := Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("unexpected diagnostics: %v", diag)
	}
	if len(f.Vars) != 2 || f.Vars[1].Name != "tickets" || !f.Vars[1].List || f.Vars[1].Elem != "ticket" {
		t.Fatalf("vars = %+v", f.Vars)
	}
	if len(f.Shapes) != 1 || f.Shapes[0].Name != "ticket" || len(f.Shapes[0].Fields) != 2 {
		t.Fatalf("shapes = %+v", f.Shapes)
	}
	blk, ok := f.Root.Members[0].(*Block)
	if !ok || blk.Kind != BlockEach || blk.Source != "tickets" || blk.Var != "t" {
		t.Fatalf("block = %+v", f.Root.Members[0])
	}
	if len(blk.Branches) != 2 || blk.Branches[1].Cond != nil {
		t.Fatalf("branches = %+v", blk.Branches)
	}
}

func TestParseEachErrors(t *testing.T) {
	cases := map[string]string{
		"{#each tickets as t}\n\t{/each}":                     "expected '.' after the bridge namespace",
		"{#each data.tickets t}\n\t{/each}":                   "expected 'as'",
		"{#each data.tickets as}\n\t{/each}":                  "expected a loop variable",
		"{#each data.tickets as t}\n{/if}":                    "unexpected {/if}",
		"{#each data.tickets as t}\n{:else if true}\n{/each}": "expected '}' after {:else}",
	}
	for body, want := range cases {
		src := "Window {\n\tColumn {\n\t\t" + body + "\n\t}\n}\n"
		_, diag := Parse([]byte(src))
		if diag.Empty() {
			t.Errorf("%q was accepted", body)
			continue
		}
		if got := diag.Format("<src>", []byte(src)); !strings.Contains(got, want) {
			t.Errorf("%q gave %s, want %q", body, got, want)
		}
	}
}

func TestParseDataErrors(t *testing.T) {
	cases := map[string]string{
		"data { rows [] }\nWindow { }":       "expected ':'",
		"data { rows: [] }\nWindow { }":      "expected a shape name",
		"data { rows: ticket[ }\nWindow { }": "expected ']'",
		"data rows\nWindow { }":              "expected '{' after the shape name",
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
	posts: [post]
	onOpen: func(id: string)
	onSave: func(title: string, count: number)
}
type post { id: string, title: string }
Window {
	Column {
		{#each bridge.posts as post}
			Button { label: post.title onClick: bridge.onOpen(post.id) }
		{/each}
		Slider { onChange: bridge.onSave("x", event) }
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
	if !posts.List || posts.Elem != "post" {
		t.Fatalf("posts = %+v", posts)
	}
	open := f.Vars[1]
	if !open.Fn || len(open.Args) != 1 || open.Args[0].Name != "id" || open.Args[0].Type != "string" {
		t.Fatalf("onOpen = %+v", open)
	}
	save := f.Vars[2]
	if len(save.Args) != 2 || save.Args[1].Name != "count" || save.Args[1].Type != "number" {
		t.Fatalf("onSave = %+v", save)
	}
	if len(f.Shapes) != 1 || f.Shapes[0].Name != "post" || len(f.Shapes[0].Fields) != 2 {
		t.Fatalf("shapes = %+v", f.Shapes)
	}
	each := f.Root.Members[0].(*Element).Members[0].(*Block)
	if each.Source != "posts" || each.Var != "post" {
		t.Fatalf("each = %+v", each)
	}
	btn := each.Branches[0].Members[0].(*Element)
	call, ok := btn.Prop("onClick").Value.(*Call)
	if !ok || call.Callee.ID != "bridge" || call.Callee.Prop != "onOpen" || len(call.Args) != 1 {
		t.Fatalf("onClick = %#v", btn.Prop("onClick").Value)
	}
}

func TestParseBridgeErrors(t *testing.T) {
	cases := map[string]string{
		"bridge { onOpen: func }\nWindow { }":     "expected '(' after func",
		"bridge { onOpen: func(id) }\nWindow { }": "expected ':' after the parameter name",
		"bridge { rows: [ ] }\nWindow { }":        "expected a shape name",
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
