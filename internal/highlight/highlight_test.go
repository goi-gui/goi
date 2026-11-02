package highlight

import (
	"strings"
	"testing"
)

// Every byte must be covered exactly once, in order: the painter walks the spans
// to draw a line and would drop any gap.
func TestSpansCoverEveryByte(t *testing.T) {
	for _, tc := range []struct{ lang, src string }{
		{"go", "// note\nfunc f() {\n\tx := \"hi\" + 42\n}"},
		{"python", "def f(x):\n    return x + 1  # sum"},
		{"golang", "type T struct{}"},
		{"js", "const a = () => 1;"},
		{"nonsense-language", "anything at all"},
		{"", "plain text"},
		{"go", "unterminated \"string"},
		{"go", ""},
	} {
		spans := Spans(tc.src, tc.lang)
		prev := 0
		for _, sp := range spans {
			if sp.Start != prev {
				t.Fatalf("%s: span %+v starts at %d, want %d", tc.lang, sp, sp.Start, prev)
			}
			if sp.End <= sp.Start {
				t.Fatalf("%s: empty span %+v", tc.lang, sp)
			}
			prev = sp.End
		}
		if prev != len(tc.src) {
			t.Fatalf("%s: spans cover %d of %d bytes", tc.lang, prev, len(tc.src))
		}
	}
}

// The classes a theme colors must actually come out of real code.
func TestSpansClassifyGo(t *testing.T) {
	src := "func add(a int) int { return a + 42 } // sum"
	spans := Spans(src, "go")
	kinds := map[Kind][]string{}
	for _, sp := range spans {
		kinds[sp.Kind] = append(kinds[sp.Kind], src[sp.Start:sp.End])
	}
	for kind, want := range map[Kind]string{
		Keyword:  "func",
		Number:   "42",
		Comment:  "// sum",
		Function: "add",
	} {
		if !strings.Contains(strings.Join(kinds[kind], " "), want) {
			t.Errorf("%v spans = %q, want it to contain %q", kind, kinds[kind], want)
		}
	}
}

// A language name is matched leniently: alias, case, or file extension.
func TestLexerNames(t *testing.T) {
	for _, name := range []string{"go", "Go", "GO", "golang", "goi", "python", "py", "js", "yaml", "yml", "toml", "css", "html"} {
		if got := lexerFor(name); got == nil {
			t.Errorf("no lexer for %q", name)
		}
	}
	if got := lexerFor("no-such-language-anywhere"); got != nil {
		t.Errorf("found a lexer for a made-up name: %v", got.Config().Name)
	}
}

// Chroma's registry is the language list the docs promise.
func TestLanguagesArePlentiful(t *testing.T) {
	if n := len(Languages()); n < 100 {
		t.Fatalf("only %d lexers", n)
	}
}
