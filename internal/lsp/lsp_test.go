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
	frag := "props { title string }\nColumn {\n\tbogus: 1\n}\n"
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

func TestCompletionOffersImportedFragments(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	writeDesign(t, dir, "Side.goi", "props { title string }\nColumn { }\n")
	src := "import \"./Side.goi\" as Side\nWindow {\n\t\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "\t\n", 1),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	for _, it := range list.Items {
		if it.Label == "Side" && it.Detail == "fragment" {
			return
		}
	}
	t.Fatalf("the imported fragment is missing from completion: %v", list.Items)
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
	fragURI := writeDesign(t, dir, "Side.goi", "props { title string }\nColumn { }\n")
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

func TestDefinitionFollowsAFragmentCall(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	fragURI := writeDesign(t, dir, "Side.goi", "props { title string }\nColumn { }\n")
	src := "import \"./Side.goi\" as Side\nWindow {\n\tSide { }\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "\tSide { }", 2),
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

// TestChildrenSlotIsAFragment keeps the editor quiet on a fragment that only
// takes children: like a file with props, it has no Window root.
func TestChildrenSlotIsAFragment(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	// The bad property is the only expected error: if the file were treated as
	// an entry point it would also complain that the root is not Window.
	src := "Column {\n\tgap: 5\n\t$children\n\tText { nope: 1 }\n}\n"
	uri := writeDesign(t, dir, "Card.goi", src)
	c.openDoc(uri, src)

	diags := c.diagnostics(uri)
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "unknown property") {
		t.Fatalf("diagnostics = %v, want one unknown property", diags)
	}
}

// TestCompletionOffersChildrenSlot keeps the slot discoverable where it is
// valid: a container body, once, and never inside a leaf.
func TestCompletionOffersChildrenSlot(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tColumn {\n\t\t$\n\t}\n}\n"
	uri := writeDesign(t, dir, "Card.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "$\n", 1),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	var slot bool
	for _, it := range list.Items {
		if it.Label == "$children" {
			slot = true
		}
		if it.Label == "Text" {
			t.Fatalf("an element was offered after '$': %v", list.Items)
		}
	}
	if !slot {
		t.Fatalf("$children missing: %v", list.Items)
	}

	// A leaf cannot hold the slot.
	leaf := "Window {\n\tText {\n\t\t$\n\t}\n}\n"
	leafURI := writeDesign(t, dir, "Leaf.goi", leaf)
	c.openDoc(leafURI, leaf)
	res = c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": leafURI},
		"position":     at(t, leaf, "$\n", 1),
	})
	list = completionList{}
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	for _, it := range list.Items {
		if it.Label == "$children" {
			t.Fatal("$children was offered inside a leaf")
		}
	}
}

// TestHoverExplainsTheSlotAndTemplate documents both new pieces where they are
// written, so neither needs the docs site to be understood.
func TestHoverExplainsTheSlotAndTemplate(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Column {\n\t$children\n\tListItem { template: true label: \"x\" }\n}\n"
	uri := writeDesign(t, dir, "Card.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "$children", 2),
	})
	var h struct {
		Contents doc `json:"contents"`
	}
	if err := json.Unmarshal(res, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "slot") {
		t.Fatalf("hover on $children = %q", h.Contents.Value)
	}

	res = c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "template", 2),
	})
	h = struct {
		Contents doc `json:"contents"`
	}{}
	if err := json.Unmarshal(res, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "ui.Fill") {
		t.Fatalf("hover on template = %q", h.Contents.Value)
	}
}

// TestIfBlockIsKnownToTheEditor covers the three things a design needs while
// typing a block: the block adds no diagnostic of its own, the body is still a
// normal element body, and the outline shows the arms' elements.
func TestIfBlockIsKnownToTheEditor(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := `Window {
	Column {
		if app.dark {
			Text { text: "dark" }
		} else {
			Text { nope: 1 text: "light" }
			
		}
	}
}
`
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	// Exactly one problem: the property inside the arm. The block itself is
	// swallowed by the parser, not reported as unknown.
	diags := c.diagnostics(uri)
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "unknown property") {
		t.Fatalf("diagnostics = %v, want one unknown property", diags)
	}

	// Completion inside an arm offers the enclosing element's properties.
	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "\t\t\t\n\t\t}", 3),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	var sawSpacing bool
	for _, it := range list.Items {
		if it.Label == "spacing" {
			sawSpacing = true
		}
	}
	if !sawSpacing {
		t.Fatalf("the arm body did not complete as a Column body: %v", list.Items)
	}

	// The outline shows both arms, so the block is navigable.
	res = c.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var syms []symbol
	if err := json.Unmarshal(res, &syms); err != nil {
		t.Fatal(err)
	}
	var block *symbol
	for i := range syms[0].Children[0].Children {
		if strings.HasPrefix(syms[0].Children[0].Children[i].Name, "if ") {
			block = &syms[0].Children[0].Children[i]
		}
	}
	if block == nil || len(block.Children) != 2 {
		t.Fatalf("outline has no block with two arms: %+v", syms[0].Children[0].Children)
	}
	if !strings.Contains(block.Name, "app.dark") {
		t.Fatalf("block label = %q, want the condition in it", block.Name)
	}
}

// TestCompletionOffersBlockTags offers the Go control statements and the
// formatf builtin at member level.
func TestCompletionOffersBlockTags(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\tColumn {\n\t\tif app.dark {\n\t\t\t\n\t\t}\n\t\t\n\t}\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	labels := func(pos position) map[string]bool {
		res := c.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     pos,
		})
		var list completionList
		if err := json.Unmarshal(res, &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, it := range list.Items {
			out[it.Label] = true
		}
		return out
	}

	inner := labels(at(t, src, "\t\t\t\n\t\t}", 3))
	if !inner["if"] || !inner["for"] || !inner["formatf"] {
		t.Fatalf("control statements missing inside a body: %v", inner)
	}
	outer := labels(at(t, src, "\t\t\n\t}\n}\n", 2))
	if !outer["if"] {
		t.Fatalf("if not offered at member level: %v", outer)
	}
}

// TestDataAndEachAreKnownToTheEditor covers the editor surface of the data
// feature: `bridge.` completes the variables, a loop variable completes its
// fields, hover explains both, and the outline names the each block.
func TestDataAndEachAreKnownToTheEditor(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := `bridge {
	rows []ticket
	loading bool
}
type ticket { title string, unread bool }
Window {
	Column {
		for _, r := range bridge.rows {
			ListItem { label: r. }
		}
		Text { text: bridge. }
	}
}
`
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	labels := func(pos position) map[string]bool {
		res := c.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     pos,
		})
		var list completionList
		if err := json.Unmarshal(res, &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, it := range list.Items {
			out[it.Label] = true
		}
		return out
	}

	// A loop variable completes the shape's fields and nothing else.
	row := labels(at(t, src, "label: r. }", 9))
	if !row["title"] || !row["unread"] {
		t.Fatalf("row fields missing: %v", row)
	}
	if row["padding"] {
		t.Fatal("a property was offered as a row field")
	}

	// `bridge.` completes the declared variables.
	vars := labels(at(t, src, "text: bridge. }", 13))
	if !vars["rows"] || !vars["loading"] {
		t.Fatalf("bridge variables missing: %v", vars)
	}

	// The only problems are the two references left open with a dot; the data
	// block, the loop variable and the block itself add none.
	diags := c.diagnostics(uri)
	if len(diags) != 2 || !strings.Contains(diags[0].Message, "after '.'") {
		t.Fatalf("diagnostics = %v, want the two open references", diags)
	}

	// The outline names the block with its list and shows the arm's elements.
	// The completion file above does not parse (open dots), and the outline is
	// only built for a file that does, so a clean copy is used here.
	clean := `bridge { rows []ticket }
type ticket { title string, unread bool }
Window {
	Column {
		for _, r := range bridge.rows {
			ListItem { label: r.title selected: r.unread }
		}
	}
}
`
	cleanURI := writeDesign(t, dir, "clean.goi", clean)
	c.openDoc(cleanURI, clean)
	res := c.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": cleanURI},
	})
	var syms []symbol
	if err := json.Unmarshal(res, &syms); err != nil {
		t.Fatal(err)
	}
	var each *symbol
	for i := range syms[0].Children[0].Children {
		if strings.HasPrefix(syms[0].Children[0].Children[i].Name, "for") {
			each = &syms[0].Children[0].Children[i]
		}
	}
	if each == nil || !strings.Contains(each.Name, "rows") {
		t.Fatalf("outline has no for loop over bridge.rows: %+v", syms[0].Children[0].Children)
	}
	if len(each.Children) != 1 || each.Children[0].Name != "ListItem" {
		t.Fatalf("each arm children = %+v", each.Children)
	}
}

// TestCompletionOffersModeGlobals keeps the two new app globals discoverable,
// with the light/dark meaning in the detail line.
func TestCompletionOffersModeGlobals(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := "Window {\n\ttheme: app.\n}\n"
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	res := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     at(t, src, "theme: app.", len("theme: app.")),
	})
	var list completionList
	if err := json.Unmarshal(res, &list); err != nil {
		t.Fatal(err)
	}
	details := map[string]string{}
	for _, it := range list.Items {
		details[it.Label] = it.Detail
	}
	for _, want := range []string{"mode", "systemMode", "theme", "dark", "os"} {
		if _, ok := details[want]; !ok {
			t.Fatalf("app.%s missing: %v", want, details)
		}
	}
	if !strings.Contains(details["mode"], "light | dark") {
		t.Fatalf("app.mode detail = %q", details["mode"])
	}
	if !strings.Contains(details["systemMode"], "light | dark") {
		t.Fatalf("app.systemMode detail = %q", details["systemMode"])
	}
}

// TestBridgeIsKnownToTheEditor covers the two new completion paths: the
// `bridge.` members are the declared handlers, and an event property offers a
// handler call rather than a literal.
func TestBridgeIsKnownToTheEditor(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := `bridge {
	onOpen func(id string)
}
Window {
	Column {
		Button { onClick:  }
		Button { onClick: bridge. }
	}
}
`
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	labels := func(pos position) map[string]bool {
		res := c.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     pos,
		})
		var list completionList
		if err := json.Unmarshal(res, &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, it := range list.Items {
			out[it.Label] = true
		}
		return out
	}

	call := labels(at(t, src, "onClick:  }", 9))
	if !call["bridge.onOpen(…)"] {
		t.Fatalf("handler call missing: %v", call)
	}
	if call["true"] {
		t.Fatal("a literal was offered for an event")
	}

	member := labels(at(t, src, "onClick: bridge. ", 16))
	if !member["onOpen"] {
		t.Fatalf("bridge members missing: %v", member)
	}
}

// TestIDReferencesAreKnownToTheEditor covers reading another node by its id:
// `width: abc.width` completes the id while it is typed, then the element's
// properties after the dot, explains either half on hover, and jumps to the
// `id:` that declares it.
func TestIDReferencesAreKnownToTheEditor(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	// The completion file leaves two values open mid-typing, which is what the
	// editor sees on every keystroke, so it need not parse.
	src := `Window {
	Rectangle {
		id: abc
		width: 200
	}
	Rectangle {
		width: ab
		height: abc.
	}
}
`
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	labels := func(pos position) map[string]bool {
		res := c.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     pos,
		})
		var list completionList
		if err := json.Unmarshal(res, &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, it := range list.Items {
			out[it.Label] = true
		}
		return out
	}

	// A value position offers the ids the file declares, beside its own values.
	value := labels(at(t, src, "width: ab\n", len("width: ab")))
	if !value["abc"] {
		t.Fatalf("id missing from a value position: %v", value)
	}

	// `abc.` offers the element's properties, common ones included.
	member := labels(at(t, src, "height: abc.\n", len("height: abc.")))
	if !member["width"] || !member["visible"] {
		t.Fatalf("id members missing: %v", member)
	}
	if member["spacing"] {
		t.Fatal("a Column property leaked into a Rectangle member list")
	}

	// Hover and go-to-definition need a file that parses, since they work off
	// the AST. The reference reads `abc.width` there.
	clean := `Window {
	Rectangle {
		id: abc
		width: 200
	}
	Rectangle {
		height: abc.width
	}
}
`
	cleanURI := writeDesign(t, dir, "clean.goi", clean)
	c.openDoc(cleanURI, clean)

	hres := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": cleanURI},
		"position":     at(t, clean, "abc.width", 1),
	})
	var h struct {
		Contents doc `json:"contents"`
	}
	if err := json.Unmarshal(hres, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "width") || !strings.Contains(h.Contents.Value, "Rectangle") {
		t.Fatalf("hover on abc. = %q", h.Contents.Value)
	}

	dres := c.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": cleanURI},
		"position":     at(t, clean, "abc.width", 1),
	})
	var locs []location
	if err := json.Unmarshal(dres, &locs); err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 {
		t.Fatalf("locations = %+v, want one", locs)
	}
	if want := pos(clean, strings.Index(clean, "id: abc")+len("id: ")); locs[0].Range.Start != want {
		t.Fatalf("definition = %+v, want the id at %+v", locs[0].Range.Start, want)
	}
}

// TestPropsAreKnownToTheEditor covers a fragment's input block: `props.`
// completes the declared props with their types, the type position completes the
// four scalar types, and hover explains the declaration.
func TestPropsAreKnownToTheEditor(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := `props {
	title string
	count float64
}
Column {
	Text { text: props. }
}
`
	uri := writeDesign(t, dir, "Card.goi", src)
	c.openDoc(uri, src)

	items := func(p position) map[string]completionResult {
		res := c.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     p,
		})
		var list completionList
		if err := json.Unmarshal(res, &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]completionResult{}
		for _, it := range list.Items {
			out[it.Label] = it
		}
		return out
	}

	member := items(at(t, src, "text: props. }", len("text: props.")))
	if !strings.Contains(member["title"].Detail, "string") || !strings.Contains(member["count"].Detail, "float64") {
		t.Fatalf("props members = %v", member)
	}

	// After a prop's name the vocabulary is Go's primitives, and the declared
	// types are suggested too.
	decl := items(at(t, src, "count float64", len("count ")))
	for _, want := range []string{"bool", "string", "float64", "int", "uint64", "color"} {
		if _, ok := decl[want]; !ok {
			t.Fatalf("type %q missing from %v", want, decl)
		}
	}

	// Hover works off the AST, so it needs a file that parses.
	clean := `props {
	title string
	count float64
}
Column {
	Text { text: props.title }
}
`
	cleanURI := writeDesign(t, dir, "Clean.goi", clean)
	c.openDoc(cleanURI, clean)
	res := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": cleanURI},
		"position":     at(t, clean, "title string", 2),
	})
	var h struct {
		Contents doc `json:"contents"`
	}
	if err := json.Unmarshal(res, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "props.title") || !strings.Contains(h.Contents.Value, "string") {
		t.Fatalf("hover on the declaration = %q", h.Contents.Value)
	}
}

// TestFormatfAndCompositePropsAreKnownToTheEditor covers the editor surface of
// the formatf builtin and of list/handler props: a reference inside the call
// still hovers, `props.` shows the composite types, and an event offers the
// fragment's own handler prop.
func TestFormatfAndCompositePropsAreKnownToTheEditor(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := `props {
	rows []ticket
	onOpen func(id string)
	count float64
}
type ticket { title string }
Column {
	Text { text: formatf("%v items", props.count) }
	Button { label: "x" onClick: props. }
	for _, row := range props.rows { row }
	for range props.
}
`
	uri := writeDesign(t, dir, "Card.goi", src)
	c.openDoc(uri, src)

	items := func(p position) map[string]completionResult {
		res := c.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     p,
		})
		var list completionList
		if err := json.Unmarshal(res, &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]completionResult{}
		for _, it := range list.Items {
			out[it.Label] = it
		}
		return out
	}

	member := items(at(t, src, "onClick: props. }", len("onClick: props.")))
	if !strings.Contains(member["rows"].Detail, "[]ticket") {
		t.Fatalf("list prop detail = %q", member["rows"].Detail)
	}
	if !strings.Contains(member["onOpen"].Detail, "func(id string)") {
		t.Fatalf("handler prop detail = %q", member["onOpen"].Detail)
	}
	if _, ok := member["count"]; !ok {
		t.Fatalf("scalar prop missing: %v", member)
	}

	// An event property offers the handler the fragment declares.
	call := items(at(t, src, "onClick: props. }", len("onClick: ")))
	if _, ok := call["props.onOpen(…)"]; !ok {
		t.Fatalf("props handler call missing: %v", call)
	}

	// Inside `range props.` the list names complete, so the loop is walkable.
	each := items(at(t, src, "range props.", len("range props.")))
	if _, ok := each["rows"]; !ok {
		t.Fatalf("props list missing from the range completion: %v", each)
	}

	// A reference inside ${...} is a real reference: hovering it works.
	clean := `props { count float64}
Column {
	Text { text: formatf("%v items", props.count) }
}
`
	cleanURI := writeDesign(t, dir, "Clean.goi", clean)
	c.openDoc(cleanURI, clean)
	res := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": cleanURI},
		"position":     at(t, clean, "props.count)", 1),
	})
	var h struct {
		Contents doc `json:"contents"`
	}
	if err := json.Unmarshal(res, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "props.count") {
		t.Fatalf("hover inside formatf = %q", h.Contents.Value)
	}
}

// TestGoDeclarationsAreKnownToTheEditor covers the Go-shaped language in the
// editor: declaration types complete, a loop variable shows what it holds, a
// shape field explains itself, and formatf is offered in a string position.
func TestGoDeclarationsAreKnownToTheEditor(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	src := `props {
	Title string
	Rows  []ticket
}
bridge {
	Count int
	OnAdd func(n int)
}
type ticket {
	title  string
	unread bool
}
Window {
	Column {
		for i, row := range props.Rows {
			Text { text: row.title }
			Text { text: i }
		}
		Text { text:  }
		Text { text: formatf("%d", bridge.Count) }
	}
}
`
	uri := writeDesign(t, dir, "Card.goi", src)
	c.openDoc(uri, src)

	items := func(p position) map[string]completionResult {
		res := c.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     p,
		})
		var list completionList
		if err := json.Unmarshal(res, &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]completionResult{}
		for _, it := range list.Items {
			out[it.Label] = it
		}
		return out
	}

	// A declaration's type position offers Go's primitives and the declared
	// types.
	types := items(at(t, src, "Count int", len("Count ")))
	for _, want := range []string{"string", "float64", "uint64", "color", "ticket", "[]", "map[string]", "func("} {
		if _, ok := types[want]; !ok {
			t.Fatalf("type %q missing from a declaration: %v", want, types)
		}
	}

	// formatf is offered where a string goes.
	values := items(at(t, src, "text:  }", len("text: ")))
	if _, ok := values["formatf"]; !ok {
		t.Fatalf("formatf missing from a string value: %v", values)
	}

	// Hover explains the field a loop variable reads. Hover works off the AST,
	// so it needs a file that parses; the completion file above does not.
	clean := `props {
	Rows []ticket
}
type ticket {
	title  string
	unread bool
}
Window {
	Column {
		for i, row := range props.Rows {
			Text { text: row.title }
		}
	}
}
`
	cleanURI := writeDesign(t, dir, "Clean.goi", clean)
	c.openDoc(cleanURI, clean)
	res := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": cleanURI},
		"position":     at(t, clean, "row.title", 1),
	})
	var h struct {
		Contents doc `json:"contents"`
	}
	if err := json.Unmarshal(res, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "row.title") || !strings.Contains(h.Contents.Value, "string") {
		t.Fatalf("hover on a loop field = %q", h.Contents.Value)
	}

	// Hover explains a shape field with its type.
	res = c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": cleanURI},
		"position":     at(t, clean, "unread bool", 2),
	})
	h = struct {
		Contents doc `json:"contents"`
	}{}
	if err := json.Unmarshal(res, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "ticket.unread") || !strings.Contains(h.Contents.Value, "bool") {
		t.Fatalf("hover on a shape field = %q", h.Contents.Value)
	}
}

// TestFragmentInstanceCompletesItsProps covers writing a call site: the body of
// `Counter { }` completes the props the fragment declares (with their Go types),
// the root's overridable properties and the slots, the value after a prop
// completes like its declared type, and hover explains both the alias and the
// prop. The fragment is read from disk, and an open buffer wins over disk.
func TestFragmentInstanceCompletesItsProps(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	frag := `props {
	Label  string
	Count  int
	OnStep func(delta int)
	Rows   []Row
}
type Row { title string }
Column {
	spacing: 4
	$header
	$children
}
`
	fragURI := writeDesign(t, dir, "Counter.goi", frag)
	src := `import "./Counter.goi" as Counter
bridge { Step int }
Window {
	Column {
		Counter {
			
		}
		Counter { Count:  }
	}
}
`
	uri := writeDesign(t, dir, "main.goi", src)
	c.openDoc(uri, src)

	items := func(p position) map[string]completionResult {
		res := c.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     p,
		})
		var list completionList
		if err := json.Unmarshal(res, &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]completionResult{}
		for _, it := range list.Items {
			out[it.Label] = it
		}
		return out
	}

	body := items(at(t, src, "\t\t\t\n", 3))
	for _, want := range []string{"Label", "Count", "OnStep", "Rows"} {
		if _, ok := body[want]; !ok {
			t.Fatalf("fragment prop %q missing from the instance body: %v", want, body)
		}
	}
	if !strings.Contains(body["Rows"].Detail, "[]Row") {
		t.Fatalf("Rows detail = %q", body["Rows"].Detail)
	}
	// The root's own properties are overridable on the call.
	if _, ok := body["spacing"]; !ok {
		t.Fatalf("the fragment root's property is missing: %v", body)
	}
	// The slots it fills are offered too.
	for _, want := range []string{"$children", "$header"} {
		if _, ok := body[want]; !ok {
			t.Fatalf("slot %q missing: %v", want, body)
		}
	}

	// A scalar prop offers the bridge values and formatf, so the instance can
	// write `Count: bridge.Step`.
	value := items(at(t, src, "Count:  }", len("Count: ")))
	if _, ok := value["bridge.Step"]; !ok {
		t.Fatalf("bridge values missing after a prop: %v", value)
	}
	if _, ok := value["formatf"]; !ok {
		t.Fatalf("formatf missing after a prop: %v", value)
	}

	// Hover explains the instance and the prop. It works off the AST, so it
	// needs a file that parses; the completion file above does not.
	clean := `import "./Counter.goi" as Counter
bridge { Step int }
Window {
	Column {
		Counter { Count: bridge.Step }
	}
}
`
	cleanURI := writeDesign(t, dir, "Clean.goi", clean)
	c.openDoc(cleanURI, clean)
	res := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": cleanURI},
		"position":     at(t, clean, "Counter { Count", 2),
	})
	var h struct {
		Contents doc `json:"contents"`
	}
	if err := json.Unmarshal(res, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "fragment") || !strings.Contains(h.Contents.Value, "Label") {
		t.Fatalf("hover on the alias = %q", h.Contents.Value)
	}
	res = c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": cleanURI},
		"position":     at(t, clean, "Count: bridge.Step", 2),
	})
	h = struct {
		Contents doc `json:"contents"`
	}{}
	if err := json.Unmarshal(res, &h); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.Contents.Value, "Count") || !strings.Contains(h.Contents.Value, "int") {
		t.Fatalf("hover on a prop = %q", h.Contents.Value)
	}

	// An open buffer wins: editing the fragment updates the call site.
	changed := `props {
	Title string
}
Column { }
`
	c.openDoc(fragURI, changed)
	body = items(at(t, src, "\t\t\t\n", 3))
	if _, ok := body["Title"]; !ok {
		t.Fatalf("the edited fragment's prop is missing: %v", body)
	}
	if _, ok := body["Label"]; ok {
		t.Fatal("a prop the buffer removed is still offered")
	}
}
