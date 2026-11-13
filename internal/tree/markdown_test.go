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

// TestMarkdownBuildsBlocks is the shape guard: each block of the document
// becomes the element that draws it.
func TestMarkdownBuildsBlocks(t *testing.T) {
	doc := "# Title\n\nHello **world**.\n\n- one\n- two\n\n```go\nfmt.Println(1)\n```\n\n---\n\n> note"
	md := markdownNode(t, `Window { Markdown { id: doc text: `+strconv.Quote(doc)+` } }`)
	got := kinds(md)
	want := []string{"Text", "Text", "Row", "Row", "Code", "Divider", "Column"}
	if len(got) != len(want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("children = %v, want %v", got, want)
		}
	}
	// The inline markup is flattened into the words, and a heading keeps its
	// level as a role.
	head := md.Kids()[0].Node().(*Text)
	if head.Text.Get() != "Title" || head.Role.Get() != "headlineLarge" {
		t.Fatalf("heading = %q role %q", head.Text.Get(), head.Role.Get())
	}
	para := md.Kids()[1].Node().(*Text)
	if para.Text.Get() != "Hello world." || !para.Wrap.Get() {
		t.Fatalf("paragraph = %q wrap %v", para.Text.Get(), para.Wrap.Get())
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
	if got := kinds(md); len(got) != 1 || got[0] != "Text" {
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
	if got := kinds(md); len(got) != 1 || got[0] != "Text" {
		t.Fatalf("children = %v, want only the paragraph", got)
	}
}
