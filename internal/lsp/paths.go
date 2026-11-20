package lsp

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type pathKind uint8

const (
	pathImport pathKind = iota
	pathSrc
)

// pathAt finds the quoted path the caret is inside: an import's string or a
// `src: "..."` value. It works on text alone, so it still answers while the
// closing quote is missing.
func pathAt(src []byte, off int) (kind pathKind, start, end int, prefix string, ok bool) {
	lineStart := 0
	if i := bytes.LastIndexByte(src[:off], '\n'); i >= 0 {
		lineStart = i + 1
	}
	lineEnd := len(src)
	if i := bytes.IndexByte(src[off:], '\n'); i >= 0 {
		lineEnd = off + i
	}
	open := -1
	for i := off - 1; i >= lineStart; i-- {
		if src[i] == '"' {
			open = i
			break
		}
	}
	if open < 0 {
		return 0, 0, 0, "", false
	}
	before := strings.TrimSpace(string(src[lineStart:open]))
	switch {
	case strings.HasPrefix(before, "import"):
		kind = pathImport
	case strings.HasSuffix(before, "src:"):
		kind = pathSrc
	default:
		return 0, 0, 0, "", false
	}
	prefix = string(src[open+1 : off])
	if kind == pathSrc && (strings.HasPrefix(prefix, "http://") ||
		strings.HasPrefix(prefix, "https://") || strings.HasPrefix(prefix, "/")) {
		// A remote or machine path is not a container path; leave it to the
		// ordinary completion rather than listing a directory it cannot mean.
		return 0, 0, 0, "", false
	}
	end = off
	for i := off; i < lineEnd; i++ {
		if src[i] == '"' {
			end = i
			break
		}
	}
	return kind, open + 1, end, prefix, true
}

// pathItems completes a container path (`goi://ui/…`) from the assets root, or
// a relative path from the document's directory. An import lists .goi files and
// directories; a src lists every file.
func (s *Server) pathItems(d *document, kind pathKind, prefix string, start, end int) []completionResult {
	root, _ := s.designRootFor(d)
	container := strings.HasPrefix(prefix, "goi://")
	typed := prefix
	if container {
		typed = strings.TrimPrefix(prefix, "goi://")
	}
	dirPart, namePart := splitPath(typed)
	dir := filepath.Join(root, filepath.FromSlash(dirPart))
	if !container {
		// An import is relative to the file; a plain src is relative to the
		// project, which is where goi run starts the program.
		base := filepath.Dir(d.path)
		if kind == pathSrc {
			base = s.projectDirFor(d)
		}
		dir = filepath.Join(base, filepath.FromSlash(dirPart))
	}
	if kind == pathImport && filepath.Clean(dir) != filepath.Clean(root) {
		if _, ok := relUnder(root, dir); !ok {
			return nil // an import cannot leave the asset container
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	idx := newLineIndex(d.text)
	var out []completionResult
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !strings.HasPrefix(name, namePart) {
			continue
		}
		dirEntry := e.IsDir()
		if kind == pathImport && !dirEntry && !strings.HasSuffix(name, ".goi") {
			continue
		}
		if kind == pathImport && !dirEntry && filepath.Clean(filepath.Join(dir, name)) == filepath.Clean(d.path) {
			continue
		}
		insert := relativeSuggestion(dirPart, name, prefix, kind)
		if container {
			insert = "goi://" + path.Join(dirPart, name)
		}
		entryKind := kindFile
		detail := "file"
		if dirEntry {
			insert += "/"
			entryKind = kindFolder
			detail = "folder"
		} else if strings.HasSuffix(name, ".goi") {
			detail = "fragment"
		}
		out = append(out, completionResult{
			Label:    insert,
			Kind:     entryKind,
			Detail:   detail,
			TextEdit: &completionTextEdit{Range: idx.at(start, end), NewText: insert},
		})
	}
	return out
}

// splitPath separates the typed directory from the part still being named.
// A path with no slash is a bare name in the current directory.
func splitPath(typed string) (dir, name string) {
	if i := strings.LastIndex(typed, "/"); i >= 0 {
		return typed[:i], typed[i+1:]
	}
	return ".", typed
}

// relativeSuggestion keeps the directory the reader typed: ./Side.goi,
// ../shared/Side.goi, or Side.goi for a bare name.
func relativeSuggestion(dirPart, name, prefix string, kind pathKind) string {
	switch {
	case dirPart == "." && strings.HasPrefix(prefix, "./"):
		return "./" + name
	case dirPart == "." && prefix == "" && kind == pathImport:
		return "./" + name
	case dirPart == ".":
		return name
	default:
		return dirPart + "/" + name
	}
}
