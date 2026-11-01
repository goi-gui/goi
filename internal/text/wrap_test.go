package text

import (
	"strings"
	"testing"
)

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

// TestFaceAdvanceCache returns the same width and shapes only once, which is
// what keeps a per-frame relayout from re-shaping every wrapped prefix.
func TestFaceAdvanceCache(t *testing.T) {
	lib := NewLibrary()
	f := lib.Face(14, WeightRegular)
	if f == nil {
		t.Skip("no font")
	}
	const s = "hello world"
	first := f.advance(s)
	if first <= 0 {
		t.Fatalf("advance = %v", first)
	}
	if got := f.advance(s); got != first {
		t.Fatalf("second call = %v, want %v", got, first)
	}
	if len(f.adv) != 1 {
		t.Fatalf("cached %d runs, want 1", len(f.adv))
	}
}

// WrapEdit must keep every byte and give exact offsets, because a caret is
// placed by them.
func TestWrapEditKeepsOffsets(t *testing.T) {
	adv := func(s string) float64 { return float64(len(s)) * 10 }
	src := "aa bb cc\n\nlongwordthatbreaks\nend"
	lines := WrapEdit(src, 60, adv)
	if len(lines) < 4 {
		t.Fatalf("got %d lines: %+v", len(lines), lines)
	}
	// Reassembling the lines with the dropped separators must give the source
	// back: a wrapped line's End is the next line's Start, and lines are joined
	// by the newline they replaced.
	var got strings.Builder
	for i, ln := range lines {
		if i > 0 && ln.Start > lines[i-1].End {
			got.WriteString(src[lines[i-1].End:ln.Start])
		}
		got.WriteString(src[ln.Start:ln.End])
	}
	if got.String() != src {
		t.Fatalf("reassembled %q, want %q", got.String(), src)
	}
	for _, ln := range lines {
		if ln.End-ln.Start != len(ln.Text) || src[ln.Start:ln.End] != ln.Text {
			t.Fatalf("line %+v does not match its offsets", ln)
		}
		if adv(ln.Text) > 60 && len(ln.Text) > 1 {
			t.Fatalf("line %q is wider than the column", ln.Text)
		}
	}
}

// A long word is broken between runes instead of running past the column.
func TestWrapEditBreaksLongWord(t *testing.T) {
	adv := func(s string) float64 { return float64(len(s)) * 10 }
	lines := WrapEdit("abcdefghij", 30, adv)
	if len(lines) != 4 {
		t.Fatalf("got %d lines: %+v", len(lines), lines)
	}
	if lines[0].Text != "abc" || lines[3].Text != "j" {
		t.Fatalf("unexpected break: %+v", lines)
	}
}
