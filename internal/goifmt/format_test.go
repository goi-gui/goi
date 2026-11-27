package goifmt

import (
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/syntax"
)

func TestFormatLayout(t *testing.T) {
	src := `import Sidebar "./Sidebar.goi"
props {
Title string
LongName int
}
bridge {
count int
onStep func(delta int)
}

type Row { a string
b int }
/* a block comment */
Window { // root
Column {
gap: 10

padding: 10 // the key and value align


Text { text: "example" }
Text{}
}
}
`
	want := `import Sidebar "./Sidebar.goi"

props {
	Title    string
	LongName int
}

bridge {
	count  int
	onStep func(delta int)
}

type Row {
	a string
	b int
}

/* a block comment */
Window { // root
	Column {
		gap:     10
		padding: 10 // the key and value align

		Text {
			text: "example"
		}

		Text {}
	}
}
`
	got := string(Source([]byte(src)))
	if got != want {
		t.Fatalf("format:\n%s\n--- want ---\n%s", got, want)
	}

	// Running it again changes nothing.
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n%s\n--- second ---\n%s", got, again)
	}
}

func TestFormatModifiers(t *testing.T) {
	src := `Window {
Column {
padding: { base: 8, md: 16 }
Text { text: conditional { true: "a" false: "b" } }
}
}
`
	want := `Window {
	Column {
		padding: responsive {
			base: 8
			md: 16
		}

		Text {
			text: conditional {
				true: "a"
				false: "b"
			}
		}
	}
}
`
	got := string(Source([]byte(src)))
	if got != want {
		t.Fatalf("format:\n%s\n--- want ---\n%s", got, want)
	}
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n%s\n--- second ---\n%s", got, again)
	}
}

func TestFormatBlocksAndHandlers(t *testing.T) {
	src := `bridge {
onOpen func(id string)
}
Window {
Column {
if app.dark {
Text { text: "dark" }
} else if app.wide {
Text { text: "wide" }
} else {
Text { text: "light" }
}
for i, row := range bridge.rows {
Button { label: row.title onClick: bridge.onOpen(row.id) }
}
for range 3 {
Rectangle { height: 1 }
}
switch bridge.role {
case "admin", "owner":
Text { text: "admin" }
case "guest":
Text { text: "guest" }
default:
Text { text: "member" }
}
}
}
`
	want := `bridge {
	onOpen func(id string)
}

Window {
	Column {
		if app.dark {
			Text {
				text: "dark"
			}
		} else if app.wide {
			Text {
				text: "wide"
			}
		} else {
			Text {
				text: "light"
			}
		}

		for i, row := range bridge.rows {
			Button {
				label:   row.title
				onClick: bridge.onOpen(row.id)
			}
		}

		for range 3 {
			Rectangle {
				height: 1
			}
		}

		switch bridge.role {
		case "admin", "owner":
			Text {
				text: "admin"
			}
		case "guest":
			Text {
				text: "guest"
			}
		default:
			Text {
				text: "member"
			}
		}
	}
}
`
	got := string(Source([]byte(src)))
	if got != want {
		t.Fatalf("format:\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatCuddlesComments keeps a comment on the line above what it explains,
// and keeps a trailing comment on its member's line.
func TestFormatCuddlesComments(t *testing.T) {
	src := `props {
// the title
Title string
}
Window {
Column {
// before a property
gap: 4 // trailing
// before a child
Text { text: "x" }
}
}
`
	got := string(Source([]byte(src)))
	for _, want := range []string{
		"props {\n\t// the title\n\tTitle string\n}",
		"// before a property\n\t\tgap: 4 // trailing",
		"// before a child\n\t\tText {",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// TestFormatKeepsBrokenSource is the safety rule: a file that does not parse is
// left exactly as it is, so a half-typed save never loses the layout.
func TestFormatKeepsBrokenSource(t *testing.T) {
	src := []byte("Window {\n\tText { text: }\n")
	if got := Source(src); string(got) != string(src) {
		t.Fatalf("a broken file was rewritten:\n%s", got)
	}
}

// TestFormatShadow keeps the CSS-style literal on one line and stable.
func TestFormatShadow(t *testing.T) {
	src := `Window {
		Text {
			text: "x"
			shadow: 2 2 5 #FF0000, -2 -2 5 theme.primary, 0 0 8 #000000 inset
		}
	}
`
	want := `Window {
	Text {
		text:   "x"
		shadow: 2 2 5 #ff0000, -2 -2 5 theme.primary, 0 0 8 #000000 inset
	}
}
`
	got := string(Source([]byte(src)))
	if got != want {
		t.Fatalf("format:\n%s\n--- want ---\n%s", got, want)
	}
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n%s", again)
	}
}

// TestRotateRoundTrips pins the formatter for the CSS rotate forms: a unit is
// written back as degrees (the same value), and `none` stays `none`.
func TestRotateRoundTrips(t *testing.T) {
	src := `Window {
	Column {
		rotate: none
		rotate: 90deg
		rotate: -45deg
		rotate: x 45deg
		rotate: 1 1 1 90deg
	}
}
`
	got := string(Source([]byte(src)))
	if got != src {
		t.Fatalf("rotate did not round-trip:\n%s", got)
	}
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("format is not idempotent:\n%s", again)
	}
	// A turn or rad value becomes the equivalent angle in degrees, which is the
	// same value written the way the renderer reads it.
	for src, want := range map[string]string{
		`Window { Column { rotate: 1.5707963267948966rad } }`: "rotate: 90deg",
		`Window { Column { rotate: 0.25turn } }`:              "rotate: 90deg",
		// A radian value that is not a round angle is rounded to seven digits,
		// which is finer than the radians the painter keeps.
		`Window { Column { rotate: 1.57rad } }`: "rotate: 89.95437deg",
	} {
		if got := string(Source([]byte(src))); !strings.Contains(got, want) {
			t.Fatalf("%s:\n%s", src, got)
		}
	}
}

// TestFormatLists keeps a list on one line with a comma and one space after
// each item, and drops the trailing comma the parser allows.
func TestFormatLists(t *testing.T) {
	src := `Window {
	PieChart {
		categories: ["Direct",  "Search"]
		ChartSeries {
			values: [42,27,18,]
			colors: [theme.primary, #c62828]
		}
	}
}
`
	want := `Window {
	PieChart {
		categories: ["Direct", "Search"]

		ChartSeries {
			values: [42, 27, 18]
			colors: [theme.primary, #c62828]
		}
	}
}
`
	if got := string(Source([]byte(src))); got != want {
		t.Fatalf("format =\n%s\nwant\n%s", got, want)
	}
}

// TestFormatMultilineString is the round trip that matters: a document is
// written one line per source line, lined up under the first line's text, and
// reading the result back gives the same value. Nested indentation and blank
// lines are part of the value and must survive.
func TestFormatMultilineString(t *testing.T) {
	src := "Window {\n" +
		"\tMarkdown {\n" +
		"\t\ttext: !\"# Title\n" +
		"\n" +
		"\t\ta paragraph\n" +
		"\t\t- one\n" +
		"\t\t\t- nested\n" +
		"\t\t\"!\n" +
		"\t}\n" +
		"}\n"
	// valueCol is the width of `text:` plus its padding, and the text starts two
	// characters later, behind `!"`.
	pad := "\t\t" + strings.Repeat(" ", 8)
	got := string(Source([]byte(src)))
	want := "Window {\n" +
		"\tMarkdown {\n" +
		"\t\ttext: !\"# Title\n" +
		"\n" +
		pad + "a paragraph\n" +
		pad + "- one\n" +
		pad + "\t- nested\n" +
		pad + "\"!\n" +
		"\t}\n" +
		"}\n"
	if got != want {
		t.Fatalf("format =\n%s\nwant\n%s", got, want)
	}
	// The formatted source parses back to the same string.
	first := parseText(t, src)
	second := parseText(t, got)
	if first != second {
		t.Fatalf("round trip changed the value: %q -> %q", first, second)
	}
}

// parseText returns the Markdown text of a design, for the round-trip check.
func parseText(t *testing.T, src string) string {
	t.Helper()
	f, diag := syntax.Parse([]byte(src))
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	var out string
	syntax.WalkElements(f.Root, func(e *syntax.Element) {
		if e.Name == "Markdown" {
			if p := e.Prop("text"); p != nil {
				out = p.Value.(*syntax.String).Val
			}
		}
	})
	return out
}

// TestMultilineAlignsWithItsValueColumn checks the alignment uses the padded
// value column: a longer property beside the text moves the block right with it.
func TestMultilineAlignsWithItsValueColumn(t *testing.T) {
	src := "Window {\n" +
		"\tMarkdown {\n" +
		"\t\tpadding: 1\n" +
		"\t\ttext: !\"one\n" +
		"\t\ttwo\"!\n" +
		"\t}\n" +
		"}\n"
	got := string(Source([]byte(src)))
	// `padding:` is 8 wide, so its value column is 9 and the text starts two
	// characters later, behind `!"`.
	pad := "\t\t" + strings.Repeat(" ", 11)
	if !strings.Contains(got, "text:    !\"one\n"+pad+"two\"!") {
		t.Fatalf("format =\n%s", got)
	}
	if parseText(t, got) != "one\ntwo" {
		t.Fatalf("round trip = %q", parseText(t, got))
	}
}

// TestFormatSlotAndLen keeps the slot pieces: a declared slot stays bare, a len
// argument keeps its `$`, and a comment above an if stays above it.
func TestFormatSlotAndLen(t *testing.T) {
	src := `Column {
	$header
	$children
	// the actions only show when the caller passes some
	if len($actions) > 0 {
		$actions
	}
}`
	got := string(Source([]byte(src)))
	want := "Column {\n" +
		"\t$header\n" +
		"\n" +
		"\t$children\n" +
		"\n" +
		"\t// the actions only show when the caller passes some\n" +
		"\tif len($actions) > 0 {\n" +
		"\t\t$actions\n" +
		"\t}\n" +
		"}\n"
	if got != want {
		t.Fatalf("format =\n%s\nwant =\n%s", got, want)
	}
}

// TestFormatScale keeps the scale spellings: percentages arrive as their
// factor, one value is one value, and the identity is written none.
func TestFormatScale(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Window { Column { scale: 2 } }", "scale: 2"},
		{"Window { Column { scale: 50% } }", "scale: 0.5"},
		{"Window { Column { scale: 2 0.5 } }", "scale: 2 0.5"},
		{"Window { Column { scale: 200% 50% 200% } }", "scale: 2 0.5 2"},
		{"Window { Column { scale: none } }", "scale: none"},
		{"Window { Column { scale: 1 } }", "scale: none"},
	} {
		got := string(Source([]byte(tc.in)))
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%q formatted as\n%s\nwant %q", tc.in, got, tc.want)
		}
	}
}

// TestFormatModulo keeps a modulo expression spaced like the other operators.
func TestFormatModulo(t *testing.T) {
	src := `Window { Column { for i := range 4 { if i%2 == 0 { Text {} } } } }`
	got := string(Source([]byte(src)))
	if !strings.Contains(got, "if i % 2 == 0 {") {
		t.Fatalf("format =\n%s", got)
	}
}

// TestFormatLoopControls keeps continue and break on their own lines, with no
// braces, inside the arm that contains them.
func TestFormatLoopControls(t *testing.T) {
	src := `Window { Column { for i := range 5 { if i == 1 { continue } if i == 3 { break } Text {} } } }`
	got := string(Source([]byte(src)))
	for _, want := range []string{"\t\t\t\tcontinue\n", "\t\t\t\tbreak\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("format =\n%s\nwant %q", got, want)
		}
	}
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n%s\n--- second ---\n%s", got, again)
	}
}

// TestFormatTransition keeps the unquoted property/duration list.
func TestFormatTransition(t *testing.T) {
	src := `Window { Rectangle { transition: width 500ms, height 500ms, background 1s } }`
	got := string(Source([]byte(src)))
	if !strings.Contains(got, "transition: width 500ms, height 500ms, background 1s") {
		t.Fatalf("format =\n%s", got)
	}
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n%s\n--- second ---\n%s", got, again)
	}
}

// TestFormatTransitionTiming keeps the timing function and delay in CSS order.
func TestFormatTransitionTiming(t *testing.T) {
	src := `Window { Rectangle { transition: width 500ms linear, height 500ms ease-in-out 100ms, opacity 1s ease-out -50ms } }`
	got := string(Source([]byte(src)))
	if !strings.Contains(got, "transition: width 500ms linear, height 500ms ease-in-out 100ms, opacity 1s ease-out -50ms") {
		t.Fatalf("format =\n%s", got)
	}
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n%s\n--- second ---\n%s", got, again)
	}
}

// TestFormatCrossfadeDuration keeps the unquoted duration on the property.
func TestFormatCrossfadeDuration(t *testing.T) {
	src := `Window { Image { crossfadeSrc: 500ms } }`
	got := string(Source([]byte(src)))
	if !strings.Contains(got, "crossfadeSrc: 500ms") {
		t.Fatalf("format =\n%s", got)
	}
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n%s\n--- second ---\n%s", got, again)
	}
}

// TestFormatThemeTransition keeps the effect and duration unquoted.
func TestFormatThemeTransition(t *testing.T) {
	src := `Window { themeTransition: crossfade 500ms }`
	got := string(Source([]byte(src)))
	if !strings.Contains(got, "themeTransition: crossfade 500ms") {
		t.Fatalf("format =\n%s", got)
	}
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n%s\n--- second ---\n%s", got, again)
	}
}

// TestFormatUnquotedGradient keeps the gradient in the spelling the parser
// now accepts and does not add quotes around it.
func TestFormatUnquotedGradient(t *testing.T) {
	src := "Window {\n\tColumn {\n\t\tbackground: linear-gradient(90deg, red 0%, blue 100%)\n\t}\n}"
	got := string(Source([]byte(src)))
	if !strings.Contains(got, "linear-gradient(90deg, red 0%, blue 100%)") {
		t.Fatalf("format =\n%s", got)
	}
	if strings.Contains(got, `"linear-gradient`) {
		t.Fatalf("gradient gained quotes:\n%s", got)
	}
	if again := string(Source([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n%s\n--- second ---\n%s", got, again)
	}
}

// TestFormatKeepsUnnamedImports: the formatter must not turn the optional alias
// into the explicit one, because the file name is the source of the name.
func TestFormatKeepsUnnamedImports(t *testing.T) {
	src := `import "./Sidebar.goi"
import Named "./Named.goi"
Window { }
`
	want := `import "./Sidebar.goi"
import Named "./Named.goi"

Window {}
`
	if got := string(Source([]byte(src))); got != want {
		t.Fatalf("format = %q\nwant %q", got, want)
	}
}

func TestFormatStateAndNestedModifier(t *testing.T) {
	src := `Window {
Column {
background: responsive { base: state { default: red hover: blue } }
}
}
`
	want := `Window {
	Column {
		background: responsive {
			base: state {
				default: red
				hover: blue
			}
		}
	}
}
`
	if got := string(Source([]byte(src))); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatKeepsColorOpacity(t *testing.T) {
	src := `Window {
	Rectangle {
		background: red / 50
		borderColor: #411f89 / 80
		shadow: 0 0 5 #000 / 25
	}
	Text { color: theme.primary / 40 }
}`
	got := string(Source([]byte(src)))
	for _, want := range []string{"#ff0000 / 50", "#411f89 / 80", "#000000 / 25", "theme.primary / 40"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted output has no %q:\n%s", want, got)
		}
	}
}

// TestSetupStaysInTheFormattedFile keeps a setup block from being dropped: the
// formatter prints its Go body through gofmt.
func TestSetupStaysInTheFormattedFile(t *testing.T) {
	src := `setup {
var name="Kaan"
func greet(n string) string{return "Hi, "+n}
}

Window { Text { text: greet(name) } }`
	got := string(Source([]byte(src)))
	for _, want := range []string{"setup {", `var name = "Kaan"`, `func greet(n string) string {`, "greet(name)"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted file lost %q:\n%s", want, got)
		}
	}
}
