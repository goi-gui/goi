//go:build nocode

package highlight

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

// Spans returns no spans in a build without the lexer: a code block draws as
// plain text, and Chroma and its regexp engine stay out of the binary.
func Spans(src, lang string) []Span { return nil }

// Aliases returns no language names in a build without the lexer.
func Aliases() map[string]string { return map[string]string{} }

// Languages returns no lexer names in a build without the lexer.
func Languages() []string { return nil }
