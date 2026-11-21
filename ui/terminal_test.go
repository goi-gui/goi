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
	in.key(tree.KeyA, tree.Mods{})
	in.Char('a')
	if got, want := out.String(), "a"; got != want {
		t.Fatalf("duplicate letter = %q, want %q", got, want)
	}
	out.Reset()
	in.CommitText(" ")
	in.key(tree.KeyC, tree.Mods{Ctrl: true})
	if got, want := out.String(), " \x03"; got != want {
		t.Fatalf("ctrl-c = %q, want %q", got, want)
	}

	term.Fit(40, 10)
	term.FeedString("prompt $ ")
	view := term.View(10)
	if got := string([]rune{view.Lines[0][0].R, view.Lines[0][7].R}); got != "p$" {
		t.Fatalf("view = %q", got)
	}
}

func TestTerminalDragSelectsAndCopies(t *testing.T) {
	_, win, in := inputFixture(t, terminalUI, 480, 320)
	term := MustByID[*Terminal](win, "term")
	term.Fit(40, 10)
	term.SetCellSize(10, 20)
	term.FeedString("abc\r\ndef")
	var clip string
	in.setClipboard = func(s string) { clip = s }
	b := absoluteRect(term)
	in.MouseDown(b.X+5, b.Y+15, 0, false)
	in.MouseMove(b.X+25, b.Y+35)
	in.MouseUp(b.X+25, b.Y+35, 0)
	in.key(tree.KeyC, tree.Mods{Ctrl: true, Shift: true})
	if want := "abc\ndef"; clip != want {
		t.Fatalf("clipboard = %q, want %q", clip, want)
	}
}

func TestTerminalLinkClickOpens(t *testing.T) {
	_, win, in := inputFixture(t, terminalUI, 480, 320)
	term := MustByID[*Terminal](win, "term")
	term.Fit(40, 10)
	term.SetCellSize(10, 20)
	term.FeedString("\x1b]8;;https://example.com\x1b\\hi\x1b]8;;\x1b\\")
	var opened string
	in.open = func(link string) error { opened = link; return nil }
	b := absoluteRect(term)
	in.MouseDown(b.X+5, b.Y+15, 0, false)
	in.MouseUp(b.X+5, b.Y+15, 0)
	if opened != "https://example.com" {
		t.Fatalf("opened = %q", opened)
	}
}
