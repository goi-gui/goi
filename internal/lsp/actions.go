package lsp

import (
	"encoding/json"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
)

// The one code action the server offers: wrap the component under the cursor or
// selection in a new component. No name is inserted; the caret lands where the
// name goes, so the reader types Row, Card or any other component.
const wrapTitle = "Wrap with component"

type codeActionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Range   rng `json:"range"`
	Context struct {
		Only        []string `json:"only"`
		Diagnostics []struct {
			Message string `json:"message"`
			Range   rng    `json:"range"`
		} `json:"diagnostics"`
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
	Edits        []wrapTextEdit          `json:"edits"`
}

type versionedTextDocumentID struct {
	URI     string `json:"uri"`
	Version *int   `json:"version,omitempty"`
}

// wrapTextEdit is one edit in whichever snippet shape the client reads. The
// 3.18 shape carries a snippet object; the older one carries newText and
// insertTextFormat 2. A client with neither gets plain text.
type wrapTextEdit struct {
	Range            rng           `json:"range"`
	NewText          string        `json:"newText,omitempty"`
	InsertTextFormat int           `json:"insertTextFormat,omitempty"`
	Snippet          *snippetValue `json:"snippet,omitempty"`
}

type snippetValue struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func (s *Server) codeActions(raw json.RawMessage) any {
	var p codeActionParams
	if json.Unmarshal(raw, &p) != nil {
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
	actions := []codeAction{}
	if wantsQuickfix(p.Context.Only) {
		actions = append(actions, s.importActions(d, file, p.Context.Diagnostics)...)
	}
	if !wantsRefactor(p.Context.Only) {
		return actions
	}
	idx := newLineIndex(d.text)
	start, end := idx.offset(p.Range.Start), idx.offset(p.Range.End)
	if end < start {
		start, end = end, start
	}
	first, last, ok := wrapElements(file.Root, d.text, idx, start, end)
	if !ok {
		return actions
	}

	// Zed reads the 3.18 SnippetTextEdit ({snippet: {kind, value}}); other
	// clients with snippet support read newText and insertTextFormat. Neither
	// inserts a name: $0 is the caret's place. A client without snippets gets a
	// literal Column and no caret movement.
	named, snippet := "$0", true
	if !s.snippetEdits {
		named, snippet = "Column", false
	}
	at, text := wrapRange(d.text, idx, first, last, named, snippet)
	edit := wrapTextEdit{Range: at}
	switch {
	case s.snippetEdits && s.zed:
		edit.Snippet = &snippetValue{Kind: "snippet", Value: text}
	case s.snippetEdits:
		edit.NewText = text
		edit.InsertTextFormat = 2
	default:
		edit.NewText = text
	}
	version := d.version
	actions = append(actions, codeAction{
		Title: wrapTitle,
		Kind:  "refactor.rewrite",
		Edit: &workspaceEdit{DocumentChanges: []textDocumentEdit{{
			TextDocument: versionedTextDocumentID{URI: d.uri, Version: &version},
			Edits:        []wrapTextEdit{edit},
		}}},
	})
	return actions
}

// importActions offers the unnamed import for an "unknown element" the checker
// reported, when a fragment beside the document derives that name.
func (s *Server) importActions(d *document, file *syntax.File, diags []struct {
	Message string `json:"message"`
	Range   rng    `json:"range"`
}) []codeAction {
	idx := newLineIndex(d.text)
	cands := s.fragmentCandidates(d, file)
	byAlias := make(map[string]fragmentCandidate, len(cands))
	for _, c := range cands {
		byAlias[c.Alias] = c
	}
	var out []codeAction
	seen := map[string]bool{}
	for _, dg := range diags {
		name, ok := unknownElement(dg.Message)
		if !ok || seen[name] {
			continue
		}
		c, ok := byAlias[name]
		if !ok {
			continue
		}
		edit, ok := importEdit(d, file, idx, c.Target)
		if !ok {
			continue
		}
		seen[name] = true
		version := d.version
		out = append(out, codeAction{
			Title: "Import \"" + c.Target + "\"",
			Kind:  "quickfix",
			Edit: &workspaceEdit{DocumentChanges: []textDocumentEdit{{
				TextDocument: versionedTextDocumentID{URI: d.uri, Version: &version},
				Edits:        []wrapTextEdit{{Range: edit.Range, NewText: edit.NewText}},
			}}},
		})
	}
	return out
}

// unknownElement reads the name out of `unknown element "Side"`, with or
// without a suggestion after it.
func unknownElement(msg string) (string, bool) {
	const prefix = `unknown element "`
	i := strings.Index(msg, prefix)
	if i < 0 {
		return "", false
	}
	rest := msg[i+len(prefix):]
	j := strings.IndexByte(rest, '"')
	if j <= 0 {
		return "", false
	}
	return rest[:j], true
}

// wantsQuickfix respects the client's `only` filter.
func wantsQuickfix(only []string) bool {
	if len(only) == 0 {
		return true
	}
	for _, kind := range only {
		if kind == "quickfix" || strings.HasPrefix(kind, "quickfix.") {
			return true
		}
	}
	return false
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

// wrapElements picks the component or the run of components to wrap.
//
// A caret, or a selection inside one component, wraps that component. A
// selection across several siblings wraps the run from the first to the last,
// so the components beside it stay where they are. The window root is never
// offered.
func wrapElements(root *syntax.Element, src []byte, idx lineIndex, start, end int) (*syntax.Element, *syntax.Element, bool) {
	if start == end {
		e := elementAtPos(root, src, idx, start)
		return e, e, e != nil && e != root
	}
	for end > start && isSpace(src[end-1]) {
		end--
	}
	if end <= start {
		return nil, nil, false
	}
	a := elementAtPos(root, src, idx, start)
	b := elementAtPos(root, src, idx, end-1)
	if a == nil || b == nil {
		return nil, nil, false
	}
	if a == b {
		return a, a, a != root
	}

	lookup := newElementLookup(root)
	lca := lookup.common(a, b)
	if lca == nil {
		return a, a, a != root
	}
	first, last := lookup.childOf(a, lca), lookup.childOf(b, lca)
	if first == nil || last == nil {
		// The selection starts or ends on the parent itself; wrap the side that
		// names a component.
		if first == nil && last != nil {
			return last, last, true
		}
		if last == nil && first != nil {
			return first, first, true
		}
		return nil, nil, false
	}
	if first == last {
		return first, first, true
	}
	if lookup.holder[first] != lookup.holder[last] {
		// Different if/else arms: one wrapper cannot hold both without crossing
		// a brace, so wrap the first component alone.
		return first, first, true
	}
	return first, last, true
}

// elementAt is the innermost element whose box holds pos. A pos inside the
// line indentation counts for the element on that line, so selecting a full
// line keeps the element rather than climbing to its parent.
func elementAtPos(root *syntax.Element, src []byte, idx lineIndex, pos int) *syntax.Element {
	var best *syntax.Element
	bestFrom := -1
	syntax.WalkElements(root, func(e *syntax.Element) {
		from, to := int(e.NameAt), int(e.Close)+1
		lineStart := idx.starts[idx.line(from)]
		if onlyWhitespace(string(src[lineStart:from])) {
			from = lineStart
		}
		if pos < from || pos >= to {
			return
		}
		if from > bestFrom {
			best, bestFrom = e, from
		}
	})
	return best
}

// elementLookup holds the element tree's parent and holder links. The holder is
// the element or if/for/switch block an element sits in directly, so two
// selected components in different arms are not wrapped together.
type elementLookup struct {
	parent map[*syntax.Element]*syntax.Element
	holder map[*syntax.Element]any
}

func newElementLookup(root *syntax.Element) *elementLookup {
	l := &elementLookup{
		parent: map[*syntax.Element]*syntax.Element{},
		holder: map[*syntax.Element]any{},
	}
	l.walk(root)
	return l
}

func (l *elementLookup) walk(e *syntax.Element) {
	for _, m := range e.Members {
		switch x := m.(type) {
		case *syntax.Element:
			l.parent[x] = e
			l.holder[x] = e
			l.walk(x)
		case *syntax.Block:
			l.walkBlock(x, e)
		}
	}
}

func (l *elementLookup) walkBlock(b *syntax.Block, owner *syntax.Element) {
	for _, br := range b.Branches {
		for _, m := range br.Members {
			switch x := m.(type) {
			case *syntax.Element:
				l.parent[x] = owner
				l.holder[x] = b
				l.walk(x)
			case *syntax.Block:
				l.walkBlock(x, owner)
			}
		}
	}
}

// common returns the nearest element that contains both a and b.
func (l *elementLookup) common(a, b *syntax.Element) *syntax.Element {
	seen := map[*syntax.Element]bool{}
	for e := a; e != nil; e = l.parent[e] {
		seen[e] = true
	}
	for e := b; e != nil; e = l.parent[e] {
		if seen[e] {
			return e
		}
	}
	return nil
}

// childOf climbs from e to the element directly inside parent.
func (l *elementLookup) childOf(e, parent *syntax.Element) *syntax.Element {
	for e != nil && l.parent[e] != parent {
		e = l.parent[e]
	}
	return e
}

// wrapRange turns the first and last selected component into one wrapper. A
// component that starts its line gets a fully indented block; an inline run
// stays on one line and goifmt can break it later.
func wrapRange(src []byte, idx lineIndex, first, last *syntax.Element, named string, snippet bool) (rng, string) {
	from, to := int(first.NameAt), int(last.Close)+1
	lineStart := idx.starts[idx.line(from)]
	prefix := src[lineStart:from]
	block := onlyWhitespace(string(prefix))
	if block {
		from = lineStart
	}
	body := string(src[from:to])
	if snippet {
		body = escapeSnippet(body)
	}
	if block {
		indent := string(prefix)
		return idx.at(from, to), indent + named + " {\n" + indentBlock(body) + "\n" + indent + "}"
	}
	return idx.at(from, to), named + " { " + body + " }"
}

// onlyWhitespace reports whether a prefix is indentation, or a selection adds
// only space or a newline around the component.
func onlyWhitespace(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isSpace(s[i]) {
			return false
		}
	}
	return true
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// indentBlock moves every non-empty line one level deeper.
func indentBlock(block string) string {
	lines := strings.Split(block, "\n")
	for i := range lines {
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
