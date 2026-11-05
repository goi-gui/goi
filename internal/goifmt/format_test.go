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
