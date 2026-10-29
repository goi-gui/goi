package text

import "testing"

// advancePretend is 10px per rune, which makes the expected break points easy to
// read: a 30px column fits three characters.
func advancePretend(s string) float64 { return float64(len([]rune(s))) * 10 }

func TestWrapLinesBreaksAtWords(t *testing.T) {
	lines := WrapLines("aaa bbb ccc", 35, advancePretend)
	want := []string{"aaa", "bbb", "ccc"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines = %q, want %q", lines, want)
		}
	}
}

// TestWrapLinesKeepsWordsTogether is the point of wrapping: a word is only
// broken when it cannot fit on a line by itself.
func TestWrapLinesKeepsWordsTogether(t *testing.T) {
	lines := WrapLines("hello world", 120, advancePretend)
	if len(lines) != 1 || lines[0] != "hello world" {
		t.Fatalf("lines = %q, want one line", lines)
	}
}

func TestWrapLinesBreaksAnOverlongWord(t *testing.T) {
	lines := WrapLines("abcdefgh", 30, advancePretend)
	if len(lines) != 3 {
		t.Fatalf("lines = %q, want three", lines)
	}
	for _, line := range lines {
		if w := advancePretend(line); w > 30 {
			t.Fatalf("line %q is %vpx, over the 30px column", line, w)
		}
	}
}

func TestWrapLinesHonoursNewlines(t *testing.T) {
	lines := WrapLines("one\ntwo", 100, advancePretend)
	if len(lines) != 2 || lines[0] != "one" || lines[1] != "two" {
		t.Fatalf("lines = %q", lines)
	}
}

func TestWrapLinesUnbounded(t *testing.T) {
	lines := WrapLines("aaa bbb", 0, advancePretend)
	if len(lines) != 1 || lines[0] != "aaa bbb" {
		t.Fatalf("lines = %q, want the whole string", lines)
	}
}
