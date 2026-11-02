// Package highlight turns source text into colored runs.
//
// The lexing itself is Chroma's: it knows a few hundred languages, and the
// alternative was a hand-written tokenizer that only ever covered the handful
// anyone remembered to add. This package only decides which theme role each
// token maps to, so the colors follow the scheme with the rest of the page.
package highlight

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Kind is the semantic class of a run, which is what a color is chosen from.
type Kind uint8

// The classes the theme has colors for.
const (
	Plain Kind = iota
	Keyword
	String
	Comment
	Number
	Function
	Type
	Attribute
	Operator
)

// Span is a colored range of the source, in byte offsets.
type Span struct {
	Start, End int
	Kind       Kind
}

// langAlias maps the names .goi files and readers actually write onto the lexer
// Chroma knows. Chroma matches by name and by file extension, but a design file
// says "goi" or "ts" rather than "Go" or "TypeScript".
var langAlias = map[string]string{
	"goi":      "go",
	"golang":   "go",
	"js":       "javascript",
	"jsx":      "javascript",
	"ts":       "typescript",
	"tsx":      "typescript",
	"py":       "python",
	"rb":       "ruby",
	"rs":       "rust",
	"sh":       "bash",
	"shell":    "bash",
	"zsh":      "bash",
	"yml":      "yaml",
	"md":       "markdown",
	"c++":      "cpp",
	"c#":       "csharp",
	"cs":       "csharp",
	"kt":       "kotlin",
	"tf":       "terraform",
	"docker":   "docker",
	"postgres": "postgresql",
	"plain":    "",
	"text":     "",
	"":         "",
}

// Spans highlights src as lang. Every byte of src is covered by exactly one
// span in order, so a painter can walk them without gaps: an unknown language,
// or one Chroma fails on, comes back as a single Plain span.
func Spans(src, lang string) []Span {
	if src == "" {
		return nil
	}
	l := lexerFor(lang)
	if l == nil {
		return []Span{{0, len(src), Plain}}
	}
	return spansOf(l, src)
}

// Languages lists the lexer names a design can use, for documentation and
// error messages.
func Languages() []string {
	return lexers.Names(false)
}

// lexerFor resolves a language name, its alias, or a file extension.
func lexerFor(lang string) chroma.Lexer {
	name := strings.ToLower(strings.TrimSpace(lang))
	if alias, ok := langAlias[name]; ok {
		if alias == "" {
			return nil
		}
		name = alias
	}
	if l := lexers.Get(name); l != nil {
		// Coalesce merges adjacent tokens of the same type, which halves the
		// spans a run-of-the-mill file produces.
		return chroma.Coalesce(l)
	}
	// "." + name makes Chroma match by extension, so "html" and "css" work
	// even when the lexer's display name differs.
	if l := lexers.Match("x." + name); l != nil {
		return chroma.Coalesce(l)
	}
	return nil
}

// spansOf tokenizes src and converts the tokens to spans.
func spansOf(l chroma.Lexer, src string) []Span {
	it, err := l.Tokenise(nil, src)
	if err != nil {
		return []Span{{0, len(src), Plain}}
	}
	tokens := it.Tokens()
	out := make([]Span, 0, len(tokens))
	at := 0
	for _, t := range tokens {
		n := len(t.Value)
		if n == 0 {
			continue
		}
		k := kindOf(t.Type)
		// A token whose value is not in the source would desync every offset
		// after it; Chroma does not do that, but a guard is cheap.
		if !strings.HasPrefix(src[at:], t.Value) {
			out = append(out, Span{at, len(src), Plain})
			return out
		}
		if len(out) > 0 && out[len(out)-1].Kind == k && out[len(out)-1].End == at {
			out[len(out)-1].End = at + n
		} else {
			out = append(out, Span{at, at + n, k})
		}
		at += n
	}
	if at < len(src) {
		if len(out) > 0 && out[len(out)-1].Kind == Plain {
			out[len(out)-1].End = len(src)
		} else {
			out = append(out, Span{at, len(src), Plain})
		}
	}
	return out
}

// kindOf maps a Chroma token type onto the theme's classes.
func kindOf(t chroma.TokenType) Kind {
	switch t.Category() {
	case chroma.Comment:
		return Comment
	case chroma.Keyword:
		return Keyword
	case chroma.Operator:
		return Operator
	case chroma.Literal:
		if t.SubCategory() == chroma.LiteralNumber {
			return Number
		}
		return String
	case chroma.Name:
		switch t.SubCategory() {
		case chroma.NameFunction:
			return Function
		case chroma.NameClass, chroma.NameNamespace, chroma.NameException:
			return Type
		case chroma.NameDecorator, chroma.NameAttribute, chroma.NameTag:
			return Attribute
		}
		// Plain identifiers stay in the body color: coloring every name makes
		// code harder to read, not easier.
		return Plain
	}
	return Plain
}
