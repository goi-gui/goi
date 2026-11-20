package lsp

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/syntax"
)

// fragmentCandidate is one .goi file beside the document that can be pulled in
// as a fragment.
type fragmentCandidate struct {
	Alias  string // element name the base name derives
	Target string // "./Sidebar.goi", relative to the document
}

// autoImportItems offers the fragment files beside the document that are not
// imported yet. Accepting one inserts the element and an unnamed import in the
// same edit.
func (s *Server) autoImportItems(d *document, file *syntax.File) []completionResult {
	cands := s.fragmentCandidates(d, file)
	if len(cands) == 0 {
		return nil
	}
	idx := newLineIndex(d.text)
	out := make([]completionResult, 0, len(cands))
	for _, c := range cands {
		edit, ok := importEdit(d, file, idx, c.Target)
		if !ok {
			continue
		}
		out = append(out, completionResult{
			Label:   c.Alias,
			Kind:    kindClass,
			Detail:  "fragment · auto import",
			Snippet: c.Alias + " {\n\t$0\n}",
			Documentation: &doc{Kind: "markdown", Value: "**" + c.Alias + "** — fragment\n\n" +
				"Adds `import \"" + c.Target + "\"` at the top and writes the element. " +
				"The import is unnamed: its element name follows the file's base name."},
			AdditionalTextEdits: []completionTextEdit{edit},
		})
	}
	return out
}

// fragmentCandidates lists the non-window .goi files in the document's own
// directory that the file does not import yet, including open unsaved buffers.
func (s *Server) fragmentCandidates(d *document, file *syntax.File) []fragmentCandidate {
	if d == nil || file == nil {
		return nil
	}
	dir := filepath.Dir(d.path)
	self := filepath.Clean(d.path)
	aliases := map[string]bool{}
	targets := map[string]bool{}
	for _, imp := range file.Imports {
		aliases[imp.Alias] = true
		targets[filepath.Clean(filepath.Join(dir, filepath.FromSlash(imp.Path)))] = true
	}

	seen := map[string]bool{self: true}
	var names []string
	texts := map[string][]byte{}
	for _, open := range s.docs {
		if filepath.Dir(open.path) != dir {
			continue
		}
		path := filepath.Clean(open.path)
		if seen[path] {
			continue
		}
		seen[path] = true
		texts[path] = open.text
		names = append(names, path)
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".goi") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if seen[path] {
				continue
			}
			seen[path] = true
			names = append(names, path)
		}
	}

	out := make([]fragmentCandidate, 0, len(names))
	for _, path := range names {
		name := filepath.Base(path)
		alias, ok := syntax.DeriveAlias(name)
		if _, builtin := schema.Lookup(alias); !ok || aliases[alias] || targets[filepath.Clean(path)] || builtin {
			continue
		}
		src, ok := texts[path]
		if !ok {
			var err error
			src, err = os.ReadFile(path)
			if err != nil {
				continue
			}
		}
		f, diag := syntax.Parse(src)
		if !diag.Empty() || f == nil || f.Root == nil || f.Root.Name == "Window" {
			continue
		}
		if importsPath(f, filepath.Dir(path), self) {
			continue // importing it would close a cycle the loader rejects
		}
		out = append(out, fragmentCandidate{
			Alias:  alias,
			Target: "./" + filepath.ToSlash(name),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	// Two files can derive the same name; the first by alias wins.
	uniq := out[:0]
	seenAlias := map[string]bool{}
	for _, c := range out {
		if seenAlias[c.Alias] {
			continue
		}
		seenAlias[c.Alias] = true
		uniq = append(uniq, c)
	}
	return uniq
}

// importsPath reports whether f imports target, so an auto-import does not
// close the simplest cycle.
func importsPath(f *syntax.File, dir, target string) bool {
	for _, imp := range f.Imports {
		if filepath.Clean(filepath.Join(dir, filepath.FromSlash(imp.Path))) == target {
			return true
		}
	}
	return false
}

// importEdit inserts `import "target"` in the file's preamble. It goes after
// the last existing import, or before the first declaration/root when there is
// none.
func importEdit(d *document, file *syntax.File, idx lineIndex, target string) (completionTextEdit, bool) {
	if d == nil || file == nil {
		return completionTextEdit{}, false
	}
	at, blank := importInsertAt(file, idx, d.text)
	if at < 0 {
		return completionTextEdit{}, false
	}
	prefix := ""
	if at == len(d.text) && at > 0 && d.text[at-1] != '\n' {
		prefix = "\n"
	}
	text := prefix + "import \"" + target + "\"\n"
	if blank {
		text += "\n"
	}
	return completionTextEdit{Range: idx.at(at, at), NewText: text}, true
}

// importInsertAt finds where an import belongs and whether a blank line should
// follow it.
func importInsertAt(file *syntax.File, idx lineIndex, src []byte) (at int, blank bool) {
	if len(file.Imports) > 0 {
		last := file.Imports[len(file.Imports)-1]
		line := idx.line(int(last.At))
		if line+1 < len(idx.starts) {
			return idx.starts[line+1], false
		}
		return len(src), false
	}
	first := -1
	take := func(p syntax.Pos) {
		if first < 0 || int(p) < first {
			first = int(p)
		}
	}
	if len(file.Props) > 0 {
		take(file.PropsAt)
	}
	if len(file.Vars) > 0 {
		take(file.BridgeAt)
	}
	if len(file.Shapes) > 0 {
		take(file.Shapes[0].NameAt)
	}
	if file.Root != nil {
		take(file.Root.NameAt)
	}
	if first < 0 {
		return -1, false
	}
	return idx.starts[idx.line(first)], true
}
