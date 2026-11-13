package tree

import (
	"strconv"
	"testing"
)

// markdownNode builds a Markdown element from a design snippet and returns it.
func markdownNode(t *testing.T, src string) *Markdown {
	t.Helper()
	tr, diag := BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	for _, n := range tr.All() {
		if md, ok := n.(*Markdown); ok {
			return md
		}
	}
	t.Fatal("no Markdown in the tree")
	return nil
}

// kinds lists the child kinds a Markdown built, for a readable assertion.
func kinds(md *Markdown) []string {
	var out []string
	for _, k := range md.Kids() {
		out = append(out, k.Node().Kind())
	}
	return out
}

// hasSpan reports whether any run matches.
func hasSpan(spans []Span, ok func(Span) bool) bool {
	for _, s := range spans {
		if ok(s) {
			return true
		}
	}
	return false
}

// TestMarkdownBuildsBlocks is the shape guard: each block of the document
// becomes the element that draws it.
func TestMarkdownBuildsBlocks(t *testing.T) {
	doc := "# Title\n\nHello **world**.\n\n- one\n- two\n\n```go\nfmt.Println(1)\n```\n\n---\n\n> note"
	md := markdownNode(t, `Window { Markdown { id: doc text: `+strconv.Quote(doc)+` } }`)
	got := kinds(md)
	want := []string{"RichText", "RichText", "Row", "Row", "Code", "Divider", "Column"}
	if len(got) != len(want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("children = %v, want %v", got, want)
		}
	}
	// A heading keeps its level as a role, and a paragraph keeps its markup as
	// runs: the bold word is its own span with the same words.
	head := md.Kids()[0].Node().(*RichText)
	if mdSpanText(head.Spans()) != "Title" || head.Role.Get() != "headlineLarge" {
		t.Fatalf("heading = %q role %q", mdSpanText(head.Spans()), head.Role.Get())
	}
	para := md.Kids()[1].Node().(*RichText)
	if mdSpanText(para.Spans()) != "Hello world." || !para.Wrap.Get() {
		t.Fatalf("paragraph = %q wrap %v", mdSpanText(para.Spans()), para.Wrap.Get())
	}
	if !hasSpan(para.Spans(), func(s Span) bool { return s.Bold && s.Text == "world" }) {
		t.Fatalf("paragraph lost its bold run: %v", para.Spans())
	}
	code := md.Kids()[4].Node().(*Code)
	if code.Text.Get() != "fmt.Println(1)" || code.Language.Get() != "go" {
		t.Fatalf("code = %q lang %q", code.Text.Get(), code.Language.Get())
	}
	// A list item is a bullet plus the text, indented by nesting depth.
	first := md.Kids()[2].Node().(*Row)
	if len(first.Kids()) != 2 {
		t.Fatalf("list item has %d children, want the bullet and the text", len(first.Kids()))
	}
	if b := first.Kids()[0].Node().(*Text); b.Text.Get() != "•" {
		t.Fatalf("bullet = %q", b.Text.Get())
	}
}

// TestMarkdownRebuildsOnlyOnChange keeps the per-frame cost at a string compare:
// the second call has nothing to do.
func TestMarkdownRebuildsOnlyOnChange(t *testing.T) {
	md := markdownNode(t, `Window { Markdown { id: doc text: "# A" } }`)
	if md.Rebuild() {
		t.Fatal("Rebuild rebuilt an unchanged source")
	}
	md.SetSource("paragraph")
	if !md.Rebuild() {
		t.Fatal("Rebuild ignored a changed source")
	}
	if got := kinds(md); len(got) != 1 || got[0] != "RichText" {
		t.Fatalf("children = %v, want one paragraph", got)
	}
	if md.Rebuild() {
		t.Fatal("Rebuild ran twice for the same source")
	}
}

// TestMarkdownNestedListsAndOrderedNumbering covers the two list shapes a
// document actually uses: an ordered run keeps its numbers, and a nested list
// indents by depth.
func TestMarkdownNestedListsAndOrderedNumbering(t *testing.T) {
	md := markdownNode(t, `Window { Markdown { id: doc text: "3. three\n4. four\n   - deep" } }`)
	kids := md.Kids()
	if len(kids) != 3 {
		t.Fatalf("children = %d, want three items", len(kids))
	}
	n := kids[0].Node().(*Row).Kids()[0].Node().(*Text)
	if n.Text.Get() != "3." {
		t.Fatalf("first marker = %q, want the list's own start number", n.Text.Get())
	}
	n = kids[1].Node().(*Row).Kids()[0].Node().(*Text)
	if n.Text.Get() != "4." {
		t.Fatalf("second marker = %q", n.Text.Get())
	}
	if kids[2].PaddingEdges().Left <= 0 {
		t.Fatalf("nested item is not indented: %v", kids[2].PaddingEdges())
	}
}

// TestMarkdownTable checks that a GFM table becomes a Table with its header and
// rows, which is what makes it interactive and themeable like any other table.
func TestMarkdownTable(t *testing.T) {
	md := markdownNode(t, `Window { Markdown { id: doc text: "| a | b |\n|---|--:|\n| 1 | 2 |" } }`)
	if got := kinds(md); len(got) != 1 || got[0] != "Table" {
		t.Fatalf("children = %v, want one Table", got)
	}
	table := md.Kids()[0].Node().(*Table)
	if got := len(table.TableColumns()); got != 2 {
		t.Fatalf("columns = %d, want 2", got)
	}
	if got := len(table.TableRows()); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
	if got := table.TableColumns()[1].Align.Get(); got != "end" {
		t.Fatalf("second column align = %q, want end from the `--:` marker", got)
	}
}

// TestMarkdownSkipsRawHTML documents the one deliberate omission: raw HTML is
// not modelled, so it draws nothing instead of leaking markup into the page.
func TestMarkdownSkipsRawHTML(t *testing.T) {
	md := markdownNode(t, `Window { Markdown { id: doc text: "<div>raw</div>\n\nafter" } }`)
	if got := kinds(md); len(got) != 1 || got[0] != "RichText" {
		t.Fatalf("children = %v, want only the paragraph", got)
	}
}

// TestMarkdownInlineRuns is the inline guard: every markup the component claims
// to keep becomes a run with that style, and the words survive.
func TestMarkdownInlineRuns(t *testing.T) {
	doc := "plain *slanted* **heavy** `code` ~~gone~~ [link](https://goi.dev) ***both***"
	md := markdownNode(t, `Window { Markdown { id: doc text: `+strconv.Quote(doc)+` } }`)
	spans := md.Kids()[0].Node().(*RichText).Spans()
	cases := []struct {
		name string
		ok   func(Span) bool
	}{
		{"italic", func(s Span) bool { return s.Italic && s.Text == "slanted" }},
		{"bold", func(s Span) bool { return s.Bold && s.Text == "heavy" }},
		{"code", func(s Span) bool { return s.Code && s.Text == "code" }},
		{"strike", func(s Span) bool { return s.Strike && s.Text == "gone" }},
		{"link", func(s Span) bool { return s.Link == "https://goi.dev" && s.Text == "link" }},
		{"nested", func(s Span) bool { return s.Bold && s.Italic && s.Text == "both" }},
	}
	for _, c := range cases {
		if !hasSpan(spans, c.ok) {
			t.Errorf("no %s run in %v", c.name, spans)
		}
	}
	// Plain text is still one run, and the paragraph reads as the source minus
	// its markup.
	if got, want := mdSpanText(spans), "plain slanted heavy code gone link both"; got != want {
		t.Fatalf("plain text = %q, want %q", got, want)
	}
}

// TestRichTextChildrenBecomeSpans covers the declarative path: a design writes
// its runs as Text children, and they arrive as spans with their style mapped.
func TestRichTextChildrenBecomeSpans(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		RichText {
			id: doc
			Text { id: a text: "plain " }
			Text { text: "bold" fontWeight: bold }
			Text { text: "it" fontStyle: italic }
			Text { text: "under" decoration: underline }
			Text { text: "gone" decoration: line-through color: #ff0000ff }
			Text { text: "role" color: tertiary }
			Text { text: "docs" link: "https://goi.dev/docs" }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, ok := tr.ByID("doc")
	if !ok {
		t.Fatal("no RichText")
	}
	rt := n.(*RichText)
	spans := rt.Spans()
	if len(spans) != 7 {
		t.Fatalf("spans = %v", spans)
	}
	want := []Span{
		{Text: "plain "},
		{Text: "bold", Bold: true},
		{Text: "it", Italic: true},
		{Text: "under", Underline: true},
		{Text: "gone", Strike: true, Color: 0xFF0000FF},
		// A role stays a role, so the run follows a theme change.
		{Text: "role", ColorRole: "tertiary"},
		// link is the run's target, which is what a click opens.
		{Text: "docs", Link: "https://goi.dev/docs"},
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Fatalf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}
	// The children were authored, not drawn: they are gone from the tree, so
	// layout, paint and id lookups do not see them.
	if got := len(rt.Kids()); got != 0 {
		t.Fatalf("RichText kept %d child node(s)", got)
	}
	if _, ok := tr.ByID("a"); ok {
		t.Fatal("a run's id is still registered after it became a span")
	}
}
