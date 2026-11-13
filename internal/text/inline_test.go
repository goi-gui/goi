package text

import (
	"strings"
	"testing"
)

// mono measures every rune at 10 wide, which makes the expected breaks readable.
func mono(s string) float64 { return float64(len([]rune(s))) * 10 }

// lines joins each line's run texts, so a test reads like the source it wrapped.
func lineTexts(lines [][]InlineRun) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		var b strings.Builder
		for _, r := range line {
			b.WriteString(r.Text)
		}
		out[i] = b.String()
	}
	return out
}

func runLines(lines [][]InlineRun) string { return strings.Join(lineTexts(lines), "|") }

func TestWrapInlineBreaksAtSpaces(t *testing.T) {
	got := runLines(WrapInline([]Inline{{Text: "one two three four"}}, 75, func(_ int, s string) float64 { return mono(s) }))
	if want := "one two|three|four"; got != want {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func TestWrapInlineKeepsStylesAndOffsets(t *testing.T) {
	lines := WrapInline([]Inline{{Text: "ab ", Style: 0}, {Text: "cd", Style: 1}}, 100, func(_ int, s string) float64 { return mono(s) })
	if len(lines) != 1 || len(lines[0]) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	if a, b := lines[0][0], lines[0][1]; a.X != 0 || b.X != 30 || b.Style != 1 || b.W != 20 {
		t.Fatalf("runs = %+v", lines[0])
	}
}

// TestWrapInlineWordAcrossRuns is the inline case markup produces: a bold word
// whose halves are separate runs still wraps as one word.
func TestWrapInlineWordAcrossRuns(t *testing.T) {
	runs := []Inline{{Text: "aa hel", Style: 0}, {Text: "lo wor", Style: 1}, {Text: "ld", Style: 2}}
	got := runLines(WrapInline(runs, 120, func(_ int, s string) float64 { return mono(s) }))
	if want := "aa hello|world"; got != want {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func TestWrapInlineForcesABreakOnNewline(t *testing.T) {
	got := runLines(WrapInline([]Inline{{Text: "a\nb"}}, 0, func(_ int, s string) float64 { return mono(s) }))
	if want := "a|b"; got != want {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

// TestWrapInlineCutsLongWords keeps the paragraph inside its column.
func TestWrapInlineCutsLongWords(t *testing.T) {
	lines := WrapInline([]Inline{{Text: "abcdefgh"}}, 30, func(_ int, s string) float64 { return mono(s) })
	if got := runLines(lines); got != "abc|def|gh" {
		t.Fatalf("lines = %q", got)
	}
	for _, line := range lines {
		for _, r := range line {
			if r.W > 30 {
				t.Fatalf("run %q is %v wide, past the 30 column", r.Text, r.W)
			}
		}
	}
}

// TestWrapInlineEmptyParagraph keeps a blank block at one line of height.
func TestWrapInlineEmptyParagraph(t *testing.T) {
	if got := WrapInline(nil, 100, func(_ int, s string) float64 { return mono(s) }); len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("lines = %v, want one empty line", got)
	}
}
