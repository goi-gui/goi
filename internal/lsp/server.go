package lsp

import (
	"bufio"
	"encoding/json"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Server is one language server instance. It owns the open documents and the
// connection, and is not safe for concurrent use: the protocol loop is
// single-threaded, which is all a design file needs.
type Server struct {
	in  *bufio.Reader
	out io.Writer

	// docs holds every open document by URI.
	docs map[string]*document
	// published records which URIs have diagnostics on the client, so a fixed
	// file gets an empty list instead of keeping a stale squiggle.
	published map[string]bool
	// snippets records whether the client asked for snippet insertions. Without
	// it a completion inserts a plain name, which is what an editor that only
	// edits text can do.
	snippets bool

	shutdown bool
}

// document is one open file.
type document struct {
	uri     string
	path    string
	version int
	text    []byte
}

// New returns a server that reads from in and writes to out.
func New(in io.Reader, out io.Writer) *Server {
	return &Server{
		in:        bufio.NewReaderSize(in, 1<<16),
		out:       out,
		docs:      map[string]*document{},
		published: map[string]bool{},
	}
}

// Run serves messages until the client sends exit or closes the stream.
func (s *Server) Run() error {
	for {
		msg, err := readMessage(s.in)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := s.handle(msg); err != nil {
			return err
		}
		if s.shutdown && msg.Method == "exit" {
			return nil
		}
	}
}

// handle dispatches one message.
func (s *Server) handle(m *message) error {
	if m.Method == "" {
		return nil // a response to something we never sent; ignore
	}
	// Notifications carry no id and must not be answered.
	if len(m.ID) == 0 {
		switch m.Method {
		case "initialized":
		case "exit":
			s.shutdown = true
		case "textDocument/didOpen":
			s.didOpen(m.Params)
		case "textDocument/didChange":
			s.didChange(m.Params)
		case "textDocument/didClose":
			s.didClose(m.Params)
		case "workspace/didChangeWatchedFiles":
			s.revalidateAll()
		case "workspace/didChangeConfiguration":
		}
		return nil
	}

	switch m.Method {
	case "initialize":
		return s.reply(m, s.initialize(m.Params))
	case "shutdown":
		s.shutdown = true
		return s.reply(m, nil)
	default:
		if s.shutdown {
			return s.fail(m, codeInvalidRequest, "server is shutting down")
		}
	}
	switch m.Method {
	case "textDocument/completion":
		return s.reply(m, s.completion(m.Params))
	case "textDocument/hover":
		return s.reply(m, s.hover(m.Params))
	case "textDocument/documentSymbol":
		return s.reply(m, s.symbols(m.Params))
	case "textDocument/foldingRange":
		return s.reply(m, s.foldingRanges(m.Params))
	case "textDocument/definition":
		return s.reply(m, s.definition(m.Params))
	case "textDocument/formatting":
		return s.reply(m, s.formatting(m.Params))
	}
	return s.fail(m, codeMethodNotFound, "unknown method "+m.Method)
}

func (s *Server) reply(m *message, result any) error {
	return writeMessage(s.out, &message{ID: m.ID, Result: result})
}

func (s *Server) fail(m *message, code int, text string) error {
	return writeMessage(s.out, &message{ID: m.ID, Error: &rpcError{Code: code, Message: text}})
}

func (s *Server) notify(method string, params any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return writeMessage(s.out, &message{Method: method, Params: body})
}

// ---------------------------------------------------------------- lifecycle

func (s *Server) initialize(raw json.RawMessage) any {
	// A client that supports snippets gets a body with its braces and a caret
	// inside; one that does not gets a plain name.
	var p struct {
		Capabilities struct {
			TextDocument struct {
				Completion struct {
					CompletionItem struct {
						SnippetSupport bool `json:"snippetSupport"`
					} `json:"completionItem"`
				} `json:"completion"`
			} `json:"textDocument"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(raw, &p) == nil {
		s.snippets = p.Capabilities.TextDocument.Completion.CompletionItem.SnippetSupport
	}
	return map[string]any{
		"capabilities": map[string]any{
			"textDocumentSync": map[string]any{
				"openClose": true,
				"change":    1, // full sync: a design file is small
			},
			"completionProvider": map[string]any{
				"triggerCharacters": []string{":", " ", "$", "."},
				"resolveProvider":   false,
			},
			"hoverProvider":              true,
			"definitionProvider":         true,
			"documentSymbolProvider":     true,
			"documentFormattingProvider": true,
			"foldingRangeProvider":       true,
		},
		"serverInfo": map[string]any{
			"name":    "goipls",
			"version": "0.1.0",
		},
	}
}

// ---------------------------------------------------------------- documents

type textDocumentItem struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	Text    string `json:"text"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version int    `json:"version"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
}

func (s *Server) didOpen(raw json.RawMessage) {
	var p didOpenParams
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	d := &document{
		uri:     p.TextDocument.URI,
		path:    uriToPath(p.TextDocument.URI),
		version: p.TextDocument.Version,
		text:    []byte(p.TextDocument.Text),
	}
	s.docs[d.uri] = d
	s.validate(d)
}

func (s *Server) didChange(raw json.RawMessage) {
	var p didChangeParams
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	d, ok := s.docs[p.TextDocument.URI]
	if !ok {
		return
	}
	// Full sync: the last change holds the whole document.
	if n := len(p.ContentChanges); n > 0 {
		d.text = []byte(p.ContentChanges[n-1].Text)
	}
	d.version = p.TextDocument.Version
	s.validate(d)
}

func (s *Server) didClose(raw json.RawMessage) {
	var p didCloseParams
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	d, ok := s.docs[p.TextDocument.URI]
	if !ok {
		return
	}
	delete(s.docs, d.uri)
	// The client forgets diagnostics for a closed file, so stop tracking them.
	delete(s.published, d.uri)
}

func (s *Server) revalidateAll() {
	for _, d := range s.docs {
		s.validate(d)
	}
}

// docFor returns the open document a request points at.
func (s *Server) docFor(raw json.RawMessage) *document {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	return s.docs[p.TextDocument.URI]
}

// requestPos is a document plus a cursor, the shape most features take.
type requestPos struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position position `json:"position"`
}

func (s *Server) atPos(raw json.RawMessage) (*document, position, bool) {
	var p requestPos
	if json.Unmarshal(raw, &p) != nil {
		return nil, position{}, false
	}
	d, ok := s.docs[p.TextDocument.URI]
	return d, p.Position, ok
}

// ---------------------------------------------------------------- diagnostics

// publish sends diagnostics for uri, or clears them when list is empty.
func (s *Server) publish(uri string, diags []diagnostic) {
	if len(diags) == 0 {
		if !s.published[uri] {
			return // never had any: no need to tell the client
		}
		delete(s.published, uri)
	} else {
		s.published[uri] = true
	}
	if diags == nil {
		diags = []diagnostic{}
	}
	_ = s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         uri,
		"diagnostics": diags,
	})
}

// ---------------------------------------------------------------- helpers

// uriToPath converts a file:// URI to a filesystem path, tolerating a plain
// path so tests need no URL escaping.
func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return strings.TrimPrefix(uri, "file://")
	}
	p := u.Path
	if p == "" {
		p = u.Opaque
	}
	// A Windows drive arrives as /C:/dir.
	if len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// pathToURI is the inverse, escaped so a space or a Turkish letter survives.
func pathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

// overlayFS serves open buffers over the directory a design lives in, so an
// unsaved edit is what validation and completion see while its imports still
// resolve from disk.
type overlayFS struct {
	dir     string
	overlay map[string][]byte
}

func (o overlayFS) Open(name string) (fs.File, error) {
	if b, ok := o.overlay[name]; ok {
		return &memFile{name: name, data: b}, nil
	}
	return os.Open(filepath.Join(o.dir, filepath.FromSlash(name)))
}

// dirOverlay builds an overlay for one document: the document's own base name
// is served from memory, everything else from dir.
func (s *Server) dirOverlay(d *document) (overlayFS, string) {
	dir := filepath.Dir(d.path)
	name := filepath.Base(d.path)
	return overlayFS{dir: dir, overlay: map[string][]byte{name: d.text}}, name
}
