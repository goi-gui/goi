package ui

import (
	"bytes"
	"testing"

	"github.com/goi-gui/goi/internal/tree"
)

const terminalUI = `Window {
	Terminal {
		id:     term
		width:  400
		height: 200
		fontSize: 14
	}
}`

// TestTerminalTakesKeyboard: clicking the terminal focuses it, text and control
// keys reach the writer, and the parser paints what the reader sent.
func TestTerminalTakesKeyboard(t *testing.T) {
	_, win, in := inputFixture(t, terminalUI, 480, 320)
	term := MustByID[*Terminal](win, "term")
	var out bytes.Buffer
	term.SetWriter(&out)

	in.setKbd(term)
	in.Char('l')
	in.Char('s')
	in.key(tree.KeyEnter, tree.Mods{})
	if got, want := out.String(), "ls\r"; got != want {
		t.Fatalf("typed = %q, want %q", got, want)
	}
	out.Reset()
	in.key(tree.KeyC, tree.Mods{Ctrl: true})
	if got, want := out.String(), "\x03"; got != want {
		t.Fatalf("ctrl-c = %q, want %q", got, want)
	}

	term.Fit(40, 10)
	term.FeedString("prompt $ ")
	view := term.View(10)
	if got := string([]rune{view.Lines[0][0].R, view.Lines[0][7].R}); got != "p$" {
		t.Fatalf("view = %q", got)
	}
}
