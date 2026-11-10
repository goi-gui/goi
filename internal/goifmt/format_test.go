package goifmt

import (
	"strings"
	"testing"
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
Window {
	// root
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
Text { text: conditional { "a": true "b": false } }
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
				"a": true
				"b": false
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
			shadow: 2 2 5 #FF0000, -2 -2 5 primary, 0 0 8 #000000 inset
		}
	}
`
	want := `Window {
	Text {
		text:   "x"
		shadow: 2 2 5 #ff0000, -2 -2 5 primary, 0 0 8 #000000 inset
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
