package tree

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTerminalParsesTextAndSGR(t *testing.T) {
	var term Terminal
	term.Fit(20, 4)
	term.FeedString("hi\x1b[31m red\x1b[0m\r\nsecond")
	view := term.View(4)
	if got := string([]rune{view.Lines[0][0].R, view.Lines[0][1].R}); got != "hi" {
		t.Fatalf("first cells = %q", got)
	}
	if view.Lines[0][3].FG == 0 || view.Lines[0][3].FG == view.Lines[0][1].FG {
		t.Fatalf("ANSI fg was not applied: %+v", view.Lines[0][3])
	}
	if got := string([]rune{view.Lines[1][0].R, view.Lines[1][1].R, view.Lines[1][2].R}); got != "sec" {
		t.Fatalf("second line = %q", got)
	}
}

func TestTerminalCursorAndErase(t *testing.T) {
	var term Terminal
	term.Fit(10, 3)
	term.FeedString("one\r\ntwo\r\nthree")
	term.FeedString("\x1b[2;2H\x1b[K")
	view := term.View(3)
	if got := string([]rune{view.Lines[1][0].R, view.Lines[1][1].R}); got != "t " {
		t.Fatalf("erase line = %q", got)
	}
	if view.CursorRow != 1 || view.CursorCol != 1 {
		t.Fatalf("cursor = %d,%d", view.CursorRow, view.CursorCol)
	}
}

func TestTerminalScrollbackAndResize(t *testing.T) {
	var term Terminal
	term.Fit(10, 2)
	term.FeedString("a\r\nb\r\nc\r\nd")
	if term.ScrollLines() == 0 {
		t.Fatal("the scrollback stayed empty")
	}
	if !term.ScrollBy(1) {
		t.Fatal("scrollback did not move")
	}
	var size TerminalSize
	term.OnResize(func(s TerminalSize) { size = s })
	term.Fit(30, 8)
	if size.Cols != 30 || size.Rows != 8 {
		t.Fatalf("resize = %+v", size)
	}
	if got := term.Grid(); got.Cols != 30 || got.Rows != 8 {
		t.Fatalf("grid = %+v", got)
	}
}

func TestTerminalAlternateScreen(t *testing.T) {
	var term Terminal
	term.Fit(10, 3)
	term.FeedString("main")
	term.FeedString("\x1b[?1049h\x1b[Halt")
	if got := term.View(3).Lines[0][0].R; got != 'a' {
		t.Fatalf("alternate screen = %q", string(got))
	}
	term.FeedString("\x1b[?1049l")
	if got := term.View(3).Lines[0][0].R; got != 'm' {
		t.Fatalf("restored screen = %q", string(got))
	}
}

func TestTerminalSelectionAndModes(t *testing.T) {
	var term Terminal
	var out strings.Builder
	term.SetWriter(&out)
	term.Fit(20, 3)
	term.FeedString("one\r\ntwo")
	term.SelectionStart(0, 0)
	term.SelectionExtend(1, 2)
	if got, want := term.SelectedText(3), "one\ntwo"; got != want {
		t.Fatalf("selected = %q, want %q", got, want)
	}
	term.FeedString("\x1b[?2004h")
	term.WritePaste("hi")
	if !strings.Contains(out.String(), "\x1b[200~hi\x1b[201~") {
		t.Fatalf("bracketed paste = %q", out.String())
	}
	out.Reset()
	term.FeedString("\x1b[?1000h\x1b[?1006h")
	term.WriteMouse(0, 1, 2, true, false)
	if got := out.String(); got != "\x1b[<0;3;2M" {
		t.Fatalf("SGR mouse = %q", got)
	}
}

func TestTerminalWideAndCombiningCells(t *testing.T) {
	var term Terminal
	term.Fit(10, 2)
	term.FeedString("a\u0301世")
	v := term.View(2)
	if v.Lines[0][0].R != 'a' || v.Lines[0][0].Comb != "\u0301" {
		t.Fatalf("base cell = %+v", v.Lines[0][0])
	}
	if v.Lines[0][1].R != '世' || !v.Lines[0][2].Cont {
		t.Fatalf("wide cells = %+v %+v", v.Lines[0][1], v.Lines[0][2])
	}
	term.SelectionStart(0, 0)
	term.SelectionExtend(0, 2)
	if got, want := term.SelectedText(2), "a\u0301世"; got != want {
		t.Fatalf("selected = %q, want %q", got, want)
	}
}

func TestTerminalOSC8Link(t *testing.T) {
	var term Terminal
	term.Fit(20, 2)
	term.FeedString("\x1b]8;;https://example.com\x1b\\hi\x1b]8;;\x1b\\")
	if got := term.LinkAt(2, 0, 0); got != "https://example.com" {
		t.Fatalf("link = %q", got)
	}
	if got := term.LinkAt(2, 0, 2); got != "" {
		t.Fatalf("link after reset = %q", got)
	}
}

func TestTerminalSixel(t *testing.T) {
	var term Terminal
	term.Fit(20, 5)
	// Two solid sixel columns of six rows, red.
	term.FeedString("\x1bPq#1;2;100;0;0#1~~\x1b\\")
	v := term.View(5)
	im := v.Lines[0][0].Image
	if im == nil {
		t.Fatal("no sixel image")
	}
	if im.W != 2 || im.H != 6 {
		t.Fatalf("image = %dx%d", im.W, im.H)
	}
	if im.Pix[0] != 255 || im.Pix[1] != 0 || im.Pix[3] != 255 {
		t.Fatalf("pixel = %v", im.Pix[:4])
	}
}

func TestTerminalDECModes(t *testing.T) {
	var term Terminal
	var out strings.Builder
	term.SetWriter(&out)
	term.Fit(20, 6)

	term.FeedString("\x1b[?1h")
	term.Key(KeyUp, Mods{})
	if got := out.String(); got != "\x1bOA" {
		t.Fatalf("application cursor key = %q", got)
	}

	out.Reset()
	term.FeedString("\x1b[2;4r\x1b[?6h\x1b[1;1H")
	if row := term.View(6).CursorRow; row != 1 {
		t.Fatalf("origin cursor row = %d, want 1", row)
	}

	term.FeedString("\x1b[?1004h")
	term.WriteFocus(true)
	if got := out.String(); got != "\x1b[I" {
		t.Fatalf("focus report = %q", got)
	}

	out.Reset()
	term.FeedString("\x1b[?1015h")
	term.WriteMouse(0, 1, 2, true, false)
	if got := out.String(); got != "\x1b[32;3;2M" {
		t.Fatalf("urxvt mouse = %q", got)
	}
}

func TestTerminalExitEvent(t *testing.T) {
	var term Terminal
	term.SetWaiter(func() (int, error) { return 7, errors.New("exit status 7") })
	done := make(chan TerminalExit, 1)
	term.OnExit(func(exit TerminalExit) { done <- exit })
	term.SetReader(strings.NewReader("exit\r\n"))
	select {
	case exit := <-done:
		if exit.Code != 7 || exit.Err != nil {
			t.Fatalf("exit = %+v, want code 7 and no error", exit)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no exit event after the reader ended")
	}
}

type exitErrReader struct{}

func (exitErrReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestTerminalExitReportsReadError(t *testing.T) {
	var term Terminal
	done := make(chan TerminalExit, 1)
	term.OnExit(func(exit TerminalExit) { done <- exit })
	term.SetReader(exitErrReader{})
	select {
	case exit := <-done:
		if exit.Code != -1 || exit.Err == nil || exit.Err.Error() != "read failed" {
			t.Fatalf("exit = %+v", exit)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no exit event after the reader failed")
	}
}

// TestTerminalClear empties the screen and scrollback, homes the cursor and
// drops the selection, so new output starts at the top-left.
func TestTerminalClear(t *testing.T) {
	var term Terminal
	term.Fit(10, 3)
	term.FeedString("one\r\ntwo\r\nthree\r\nfour\r\nfive")
	if term.ScrollLines() == 0 {
		t.Fatal("the scrollback stayed empty")
	}
	term.SelectionStart(0, 0)
	term.SelectionExtend(1, 2)

	term.Clear()
	if got := term.ScrollLines(); got != 0 {
		t.Fatalf("scrollback after Clear = %d, want 0", got)
	}
	view := term.View(3)
	for r, line := range view.Lines {
		for c, cell := range line {
			if cell.R != ' ' {
				t.Fatalf("cell %d,%d = %q after Clear", r, c, string(cell.R))
			}
		}
	}
	if view.CursorRow != 0 || view.CursorCol != 0 {
		t.Fatalf("cursor after Clear = %d,%d, want 0,0", view.CursorRow, view.CursorCol)
	}
	if _, _, _, _, ok := term.Selection(); ok {
		t.Fatal("Clear kept the selection")
	}

	term.FeedString("new")
	if got := term.View(3).Lines[0][0].R; got != 'n' {
		t.Fatalf("first cell after Clear+feed = %q, want n", string(got))
	}
}

// TestTerminalClearAlt clears the screen the reader is on; the main screen
// stays for exitAlt, the way a terminal command works.
func TestTerminalClearAlt(t *testing.T) {
	var term Terminal
	term.Fit(10, 3)
	term.FeedString("main")
	term.FeedString("\x1b[?1049h\x1b[Halt")

	term.Clear()
	if got := term.View(3).Lines[0][0].R; got != ' ' {
		t.Fatalf("alternate screen after Clear = %q, want blank", string(got))
	}

	term.FeedString("\x1b[?1049l")
	if got := term.View(3).Lines[0][0].R; got != 'm' {
		t.Fatalf("restored main screen = %q, want m", string(got))
	}
}
