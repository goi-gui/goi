package lsp

import (
	"encoding/json"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
)

// The one code action the server offers: wrap the element around the cursor or
// selection in a new element. The wrapper's name is a snippet placeholder, so
// the client puts the caret on it and the reader types Row, Card or any other
// element.
const wrapTitle = "Wrap with element"

type codeActionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Range   rng `json:"range"`
	Context struct {
		Only []string `json:"only"`
	} `json:"context"`
}

type codeAction struct {
	Title string         `json:"title"`
	Kind  string         `json:"kind,omitempty"`
	Edit  *workspaceEdit `json:"edit,omitempty"`
}

type workspaceEdit struct {
	DocumentChanges []textDocumentEdit `json:"documentChanges"`
}

type textDocumentEdit struct {
	TextDocument versionedTextDocumentID `json:"textDocument"`
	Edits        []snippetTextEdit       `json:"edits"`
}

type versionedTextDocumentID struct {
	URI     string `json:"uri"`
	Version *int   `json:"version,omitempty"`
}

// snippetTextEdit is a TextEdit whose newText is a snippet: insertTextFormat 2
// is Snippet, which is what lets $1 become the caret.
type snippetTextEdit struct {
	Range            rng    `json:"range"`
	NewText          string `json:"newText"`
	InsertTextFormat int    `json:"insertTextFormat"`
}

func (s *Server) codeActions(raw json.RawMessage) any {
	var p codeActionParams
	if json.Unmarshal(raw, &p) != nil || !wantsRefactor(p.Context.Only) {
		return []codeAction{}
	}
	d := s.docs[p.TextDocument.URI]
	if d == nil {
		return []codeAction{}
	}
	file, diag := syntax.Parse(d.text)
	if !diag.Empty() || file == nil || file.Root == nil {
		return []codeAction{}
	}
	idx := newLineIndex(d.text)
	start, end := idx.offset(p.Range.Start), idx.offset(p.Range.End)
	if end < start {
		start, end = end, start
	}
	e := elementForRange(file.Root, d.text, idx, start, end)
	if e == nil || e == file.Root {
		return []codeAction{}
	}
	named, format, snippet := "${1:Column}", 2, true
	if !s.snippetEdits {
		named, format, snippet = "Column", 1, false
	}
	version := d.version
	at, text := wrapEdit(d.text, idx, e, named, snippet)
	return []codeAction{{
		Title: wrapTitle,
		Kind:  "refactor.rewrite",
		Edit: &workspaceEdit{DocumentChanges: []textDocumentEdit{{
			TextDocument: versionedTextDocumentID{URI: d.uri, Version: &version},
			Edits: []snippetTextEdit{{
				Range:            at,
				NewText:          text,
				InsertTextFormat: format,
			}},
		}}},
	}}
}

// wantsRefactor respects the client's `only` filter; no filter means offer it.
func wantsRefactor(only []string) bool {
	if len(only) == 0 {
		return true
	}
	for _, kind := range only {
		if kind == "refactor" || strings.HasPrefix(kind, "refactor.") {
			return true
		}
	}
	return false
}

// elementForRange picks the innermost element that holds the whole selection.
// A line selected with its indentation counts as the element, and a newline
// selected after the closing brace does too. A caret just past a brace is
// outside that element, so a click after a row does not offer to wrap it.
func elementForRange(root *syntax.Element, src []byte, idx lineIndex, start, end int) *syntax.Element {
	var best *syntax.Element
	syntax.WalkElements(root, func(e *syntax.Element) {
		from, to := int(e.NameAt), int(e.Close)+1
		lineStart := idx.starts[idx.line(int(e.NameAt))]
		if onlyWhitespace(string(src[lineStart:from])) {
			from = lineStart
		}
		selEnd := end
		if selEnd > to && onlyWhitespace(string(src[to:selEnd])) {
			selEnd = to
		}
		if start < from || selEnd > to || (start == end && start == to) {
			return
		}
		if best == nil || from >= int(best.NameAt) {
			best = e
		}
	})
	return best
}

// wrapEdit builds the snippet and the range it replaces. An element that starts
// its line gets a fully indented two-line wrapper; an inline element keeps its
// text on one line, and the goifmt pass puts it on its own lines later.
func wrapEdit(src []byte, idx lineIndex, e *syntax.Element, named string, snippet bool) (rng, string) {
	start, end := int(e.NameAt), int(e.Close)+1
	lineStart := idx.starts[idx.line(start)]
	indent := string(src[lineStart:start])
	body := string(src[start:end])
	if snippet {
		body = escapeSnippet(body)
	}
	if onlyWhitespace(indent) {
		return idx.at(lineStart, end),
			indent + named + " {\n" + indent + "\t" + indentLines(body) + "\n" + indent + "}"
	}
	return idx.at(start, end), named + " { " + body + " }"
}

// onlyWhitespace reports whether an element starts after indentation alone, or
// a selection adds only space or a newline around it.
func onlyWhitespace(s string) bool {
	for _, c := range s {
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
	}
	return true
}

// indentLines moves every line after the first one level deeper.
func indentLines(body string) string {
	lines := strings.Split(body, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			lines[i] = "\t" + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// escapeSnippet keeps a design's own $ and \ literal inside the snippet. goi
// writes panel references as $popover and may hold a backslash in a string.
func escapeSnippet(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `$`, `\$`)
}
