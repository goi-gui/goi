package tree

import "testing"

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
