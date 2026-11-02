package lsp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// client drives a server over in-memory pipes, so the tests speak the real
// protocol instead of calling handlers directly.
//
// A reader goroutine drains everything the server writes into a queue. Without
// it a notification the test is not waiting for (diagnostics) would block the
// server inside io.Pipe and wedge the next request.
type client struct {
	t    *testing.T
	to   io.Writer
	next int

	mu    sync.Mutex
	queue []*message
}

func newClient(t *testing.T) *client {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	srv := New(c2sR, s2cW)
	go func() { _ = srv.Run() }()

	c := &client{t: t, to: c2sW}
	go func() {
		r := bufio.NewReader(s2cR)
		for {
			m, err := readMessage(r)
			if err != nil {
				return
			}
			c.mu.Lock()
			c.queue = append(c.queue, m)
			c.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = c2sW.Close()
		_ = s2cW.Close()
	})
	return c
}

// send writes a message.
func (c *client) send(m *message) {
	c.t.Helper()
	if err := writeMessage(c.to, m); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// notify sends a notification.
func (c *client) notify(method string, params any) {
	c.t.Helper()
	body, err := json.Marshal(params)
	if err != nil {
		c.t.Fatal(err)
	}
	c.send(&message{Method: method, Params: body})
}

// request sends a request and returns the decoded result, skipping the
// notifications that arrive in between.
func (c *client) request(method string, params any) json.RawMessage {
	c.t.Helper()
	body, err := json.Marshal(params)
	if err != nil {
		c.t.Fatal(err)
	}
	c.next++
	id, _ := json.Marshal(c.next)
	c.send(&message{ID: id, Method: method, Params: body})
	for {
		m := c.nextMatching(func(m *message) bool { return string(m.ID) == string(id) })
		if m.Error != nil {
			c.t.Fatalf("%s: %s", method, m.Error.Message)
		}
		raw, err := json.Marshal(m.Result)
		if err != nil {
			c.t.Fatal(err)
		}
		return raw
	}
}

// nextMatching waits for a queued message that matches, dropping the rest.
func (c *client) nextMatching(want func(*message) bool) *message {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		for i, m := range c.queue {
			if want(m) {
				c.queue = append(c.queue[:i], c.queue[i+1:]...)
				c.mu.Unlock()
				return m
			}
		}
		c.mu.Unlock()
		if time.Now().After(deadline) {
			c.t.Fatal("timed out waiting for a message")
		}
		time.Sleep(time.Millisecond)
	}
}

// diagnostics waits for publishDiagnostics for uri.
func (c *client) diagnostics(uri string) []diagnostic {
	c.t.Helper()
	m := c.nextMatching(func(m *message) bool {
		if m.Method != "textDocument/publishDiagnostics" {
			return false
		}
		var p struct {
			URI string `json:"uri"`
		}
		return json.Unmarshal(m.Params, &p) == nil && p.URI == uri
	})
	var p struct {
		Diagnostics []diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil {
		c.t.Fatal(err)
	}
	return p.Diagnostics
}

// writeDesign writes src into dir/name and returns its URI.
func writeDesign(t *testing.T, dir, name, src string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return pathToURI(filepath.Join(dir, name))
}

// openDoc opens a document on the server.
func (c *client) openDoc(uri, text string) {
	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	})
}

// changeDoc sends a full-sync change, the only mode the server advertises.
func (c *client) changeDoc(uri, text string) {
	c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": text}},
	})
}

// pos converts a byte offset in src into a position.
func pos(src string, off int) position {
	return newLineIndex([]byte(src)).position(off)
}

// at finds the position of the first occurrence of needle in src.
func at(t *testing.T, src, needle string, delta int) position {
	t.Helper()
	i := strings.Index(src, needle)
	if i < 0 {
		t.Fatalf("%q not in source", needle)
	}
	return pos(src, i+delta)
}

func TestDiagnosticsReportUnknownProperty(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tid: main\n\tnope: 1\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	diags := c.diagnostics(uri)
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %v, want one", diags)
	}
	if !strings.Contains(diags[0].Message, "unknown property") {
		t.Fatalf("message = %q", diags[0].Message)
	}
	// The range must cover the offending property, not the whole file.
	if diags[0].Range.Start.Line != 2 || diags[0].Range.Start.Character != 1 {
		t.Fatalf("range start = %+v, want line 2 col 1", diags[0].Range.Start)
	}
}

// TestDiagnosticsClearWhenFixed guards the empty-list publish: a client keeps the
// last squiggle until it is told the file is clean.
func TestDiagnosticsClearWhenFixed(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tnope: 1\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	if diags := c.diagnostics(uri); len(diags) != 1 {
		t.Fatalf("diagnostics = %v, want one", diags)
	}
	c.changeDoc(uri, "Window {\n\tid: main\n}\n")
	if diags := c.diagnostics(uri); len(diags) != 0 {
		t.Fatalf("stale diagnostics = %v", diags)
	}
}

func TestDiagnosticsLandInTheFragment(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	frag := "param title: \"\"\nColumn {\n\tbogus: 1\n}\n"
	fragURI := writeDesign(t, dir, "Side.goi", frag)
	src := "import \"./Side.goi\" as Side\nWindow {\n\tid: main\n\tSide { title: \"x\" }\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	diags := c.diagnostics(fragURI)
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "unknown property") {
		t.Fatalf("fragment diagnostics = %v", diags)
	}
	if diags[0].Range.Start.Line != 2 {
		t.Fatalf("range start = %+v, want line 2", diags[0].Range.Start)
	}
}

func TestCompletionOffersPropertiesAndChildren(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tColumn {\n\t\t\n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "\t\t\n", 2),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, it := range list.Items {
		labels[it.Label] = true
	}
	if !labels["spacing"] || !labels["padding"] {
		t.Fatalf("properties missing: %v", list.Items)
	}
	if !labels["Button"] || !labels["Text"] {
		t.Fatalf("child elements missing: %v", list.Items)
	}
}

// TestTitleBarVocabulary keeps the editor honest about the custom title bar:
// the element, its property on Window and the OS global must all be in the
// schema the server imports, or a design that draws its own frame is covered in
// red squiggles.
func TestTitleBarVocabulary(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := `Window {
	titleBar: custom
	Column {
		TitleBar {
			visible: app.os == "linux"
			height: 44

			IconButton { icon: close }
		}
	}
}
`
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	// The property is known and enum-checked: a typo reports the accepted
	// values instead of "unknown property".
	bad := `Window {
	titleBar: nope
}
`
	badURI := writeDesign(t, dir, "bad.goi", bad)
	c.openDoc(badURI, bad)
	diags := c.diagnostics(badURI)
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "titleBar") {
		t.Fatalf("diagnostics = %v, want one about titleBar", diags)
	}

	// Hover documents the property, so the two modes are discoverable.
	hres := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "titleBar", 2),
	})
	var hovered struct {
		Contents doc `json:"contents"`
	}
	if err := json.Unmarshal(hres, &hovered); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hovered.Contents.Value, "titleBar") {
		t.Fatalf("hover = %q", hovered.Contents.Value)
	}

	// The element is a child candidate inside a container.
	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "height: 44\n\n", len("height: 44\n\n")),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	var titleBar bool
	for _, it := range list.Items {
		if it.Label == "TitleBar" {
			titleBar = true
		}
	}
	if !titleBar {
		t.Fatalf("TitleBar missing from %d children", len(list.Items))
	}
}

// TestCompletionOffersGlobalMembers covers `app.` and `window.`: the closed set
// an expression may read, so `app.os` is discoverable instead of guesswork.
func TestCompletionOffersGlobalMembers(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tTitleBar {\n\t\tvisible: app.\n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "visible: app.", len("visible: app.")),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, it := range list.Items {
		labels[it.Label] = true
	}
	for _, want := range []string{"theme", "dark", "os"} {
		if !labels[want] {
			t.Fatalf("app.%s missing from %v", want, labels)
		}
	}
	if labels["width"] {
		t.Fatal("window members leaked into app")
	}

	// window keeps its own pair.
	src2 := "Window {\n\tTitleBar {\n\t\twidth: window.\n\t}\n}\n"
	uri2 := writeDesign(t, dir, "two.goi", src2)
	c.openDoc(uri2, src2)
	res = c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri2},
		"position":     at(t, src2, "width: window.", len("width: window.")),
	})
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	labels = map[string]bool{}
	for _, it := range list.Items {
		labels[it.Label] = true
	}
	if !labels["width"] || !labels["height"] || labels["os"] {
		t.Fatalf("window members = %v", labels)
	}
}

func TestCompletionOffersEnumValues(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tButton {\n\t\tvariant: \n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "variant: ", len("variant: ")),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	var tonal bool
	for _, it := range list.Items {
		if it.Label == "tonal" {
			tonal = true
		}
		if it.Label == "spacing" {
			t.Fatal("a property was offered in value position")
		}
	}
	if !tonal {
		t.Fatalf("variant values missing: %v", list.Items)
	}
}

func TestCompletionOffersBreakpointsInsideMap(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tColumn {\n\t\tpadding: { \n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "padding: { ", len("padding: { ")),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	var base bool
	for _, it := range list.Items {
		if it.Label == "base" {
			base = true
		}
	}
	if !base {
		t.Fatalf("breakpoints missing: %v", list.Items)
	}
}

func TestCompletionOffersIconNames(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tIconButton {\n\t\ticon: ho\n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "icon: ho", len("icon: ho")),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	var home bool
	for _, it := range list.Items {
		if !strings.HasPrefix(it.Label, "ho") {
			t.Fatalf("unfiltered item %q", it.Label)
		}
		if it.Label == "home" {
			home = true
		}
	}
	if !home {
		t.Fatalf("home missing from %d items", len(list.Items))
	}
}

// TestIconItemsThatNeedQuotes keeps `icon: 10k` from lexing as a number.
func TestIconItemsThatNeedQuotes(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tIconButton {\n\t\ticon: 10\n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "icon: 10", len("icon: 10")),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range list.Items {
		if it.Label == "10k" {
			found = true
			if it.InsertText != `"10k"` {
				t.Fatalf("insertText = %q, want a quoted name", it.InsertText)
			}
		}
	}
	if !found {
		t.Fatalf("10k missing from %d items", len(list.Items))
	}
}

// TestIconListIsCapped keeps one keystroke from carrying every icon name.
func TestIconListIsCapped(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tIconButton {\n\t\ticon: \n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "icon: ", len("icon: ")),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != maxIconItems || !list.IsIncomplete {
		t.Fatalf("items = %d, incomplete = %v, want %d and true", len(list.Items), list.IsIncomplete, maxIconItems)
	}
}

func TestHoverDocumentsAProperty(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tColumn {\n\t\tspacing: 8\n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "spacing", 2),
	})
	var h struct {
		Contents doc `json:"contents"`
	}
	if err := json.Unmarshal(res, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "spacing") || !strings.Contains(h.Contents.Value, "number") {
		t.Fatalf("hover = %q", h.Contents.Value)
	}
}

func TestSymbolsMirrorTheElementTree(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tColumn {\n\t\tCard {\n\t\t\tText { text: \"x\" }\n\t\t}\n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var syms []symbol
	if err := json.Unmarshal(res, &syms); err != nil {
		t.Fatal(err)
	}
	if len(syms) != 1 || syms[0].Name != "Window" {
		t.Fatalf("symbols = %+v", syms)
	}
	if len(syms[0].Children) != 1 || syms[0].Children[0].Name != "Column" {
		t.Fatalf("children = %+v", syms[0].Children)
	}
	grand := syms[0].Children[0].Children
	if len(grand) != 1 || grand[0].Name != "Card" || len(grand[0].Children) != 1 {
		t.Fatalf("nested = %+v", grand)
	}
}

func TestDefinitionFollowsAnImport(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	fragURI := writeDesign(t, dir, "Side.goi", "param title: \"\"\nColumn { }\n")
	src := "import \"./Side.goi\" as Side\nWindow {\n\tid: main\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "Side.goi", 3),
	})
	var locs []location
	if err := json.Unmarshal(res, &locs); err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 || locs[0].URI != fragURI {
		t.Fatalf("locations = %+v, want %s", locs, fragURI)
	}
}

func TestFoldingCoversElements(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tColumn {\n\t\tid: a\n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/foldingRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var fr []foldingRange
	if err := json.Unmarshal(res, &fr); err != nil {
		t.Fatal(err)
	}
	if len(fr) != 2 {
		t.Fatalf("folding ranges = %+v, want two elements", fr)
	}
	if fr[0].StartLine != 0 || fr[0].EndLine != 4 {
		t.Fatalf("outer range = %+v", fr[0])
	}
}

// TestPositionsUseUTF16 keeps the byte-offset to column conversion honest: a
// Turkish letter and an emoji are two UTF-16 units apart from their byte length.
func TestPositionsUseUTF16(t *testing.T) {
	src := "Text { text: \"şğü😀\" }\n"
	idx := newLineIndex([]byte(src))
	off := strings.Index(src, "}") // the brace after the string
	p := idx.position(off)
	if p.Line != 0 {
		t.Fatalf("line = %d", p.Line)
	}
	// The prefix before the brace is 20 runes; the emoji counts as two UTF-16
	// units, so the column is 21.
	if p.Character != 21 {
		t.Fatalf("character = %d, want 21", p.Character)
	}
	if got := idx.offset(p); got != off {
		t.Fatalf("round trip = %d, want %d", got, off)
	}
}

// TestCompletionOffersCodeLanguages: `language` is a plain string in the
// schema, so without a special case the editor would offer no values at all.
func TestCompletionOffersCodeLanguages(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tCode {\n\t\tlanguage: g\n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "language: g", len("language: g")),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	var goLexer bool
	for _, it := range list.Items {
		if !strings.HasPrefix(it.Label, "g") {
			t.Fatalf("unfiltered item %q", it.Label)
		}
		if it.Label == "go" {
			goLexer = true
		}
	}
	if !goLexer {
		t.Fatalf("go missing from %d items", len(list.Items))
	}
}

// TestLanguageItemsThatNeedQuotes: `c++` cannot be a bare .goi identifier, so
// accepting the item must insert quotes or the file stops parsing.
func TestLanguageItemsThatNeedQuotes(t *testing.T) {
	out, _ := languageItems("c++", false)
	if len(out) == 0 {
		t.Fatal("no language matched c++")
	}
	if got := out[0].InsertText; got != `"c++"` {
		t.Fatalf("insertText = %q, want a quoted name", got)
	}
}
