package tree

import (
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/schema"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Markdown renders a markdown document as a stack of ordinary elements: a
// heading or a paragraph becomes a RichText, a fenced block a Code, a rule a
// Divider, a list a run of rows, and a table a Table. The children are real
// nodes, so layout, themes, wrapping and hit-testing work without a second
// renderer, and inline markup survives as styled runs.
//
// The markdown is parsed once per change: Rebuild compares the source it last
// rendered with the property and returns immediately when they match, which is
// what makes it safe to call on every frame.
type Markdown struct {
	Component
	// rendered is the source the children were built from.
	rendered string
}

// Kind implements Node.
func (*Markdown) Kind() string { return "Markdown" }

func (c *Markdown) namedProps() []NamedProp { return c.props("Markdown") }

// Source is the markdown the element draws.
func (c *Markdown) Source() string { return c.Text.Get() }

// SetSource replaces the markdown. The next frame rebuilds the children.
func (c *Markdown) SetSource(s string) { c.Text.Set(s) }

// Rebuild turns the markdown into child elements. It reports whether the
// children changed, so a caller can ask for a frame only when there is one.
func (c *Markdown) Rebuild() bool {
	src := c.Text.Get()
	if src == c.rendered {
		return false
	}
	c.rendered = src
	b := &c.NodeBase
	for _, k := range b.kids {
		k.parent = nil
	}
	b.kids = b.kids[:0]
	for _, n := range mdNodes(c, parseMarkdown(src)) {
		nb := n.Base()
		nb.node = n
		nb.parent = b
		nb.tree = b.tree
		nb.scope = b.scope
		nb.hooks = b.hooks
		b.kids = append(b.kids, nb)
	}
	b.MarkDirty(DirtyLayout)
	return true
}

// mdNodes builds the elements for a block list, the first one without a top
// margin.
func mdNodes(c *Markdown, blocks []mdBlock) []Node {
	var out []Node
	for i, blk := range blocks {
		out = append(out, c.buildBlock(blk, i > 0)...)
	}
	return out
}

// Block gaps in dp. A heading needs more air above it than a paragraph, and a
// code block or a quote needs a border of its own.
const (
	mdParaGap    = 8.0
	mdHeadingGap = 16.0
	mdBlockGap   = 12.0
)

// headingRole is the type role a heading level draws with.
func headingRole(level int) string {
	switch level {
	case 1:
		return "headlineLarge"
	case 2:
		return "headlineMedium"
	case 3:
		return "headlineSmall"
	case 4:
		return "titleLarge"
	case 5:
		return "titleMedium"
	}
	return "titleSmall"
}

// buildBlock turns one parsed block into elements. spaced is whether the block
// needs a top margin, which is everything after the first.
func (c *Markdown) buildBlock(blk mdBlock, spaced bool) []Node {
	switch blk.kind {
	case mdHeading:
		t := mdNew("RichText").(*RichText)
		t.SetSpans(blk.spans)
		t.Role.Set(headingRole(blk.level))
		t.Wrap.Set(true)
		if spaced {
			t.marginTop.Set(mdHeadingGap)
		}
		return []Node{t}
	case mdParagraph:
		t := mdNew("RichText").(*RichText)
		t.SetSpans(blk.spans)
		t.Wrap.Set(true)
		if spaced {
			t.marginTop.Set(mdParaGap)
		}
		return []Node{t}
	case mdCode:
		code := mdNew("Code").(*Code)
		code.Text.Set(strings.TrimRight(blk.text, "\n"))
		code.Language.Set(blk.lang)
		if spaced {
			code.marginTop.Set(mdBlockGap)
		}
		return []Node{code}
	case mdRule:
		d := mdNew("Divider").(*Divider)
		if spaced {
			d.marginTop.Set(mdBlockGap)
		}
		return []Node{d}
	case mdList:
		out := make([]Node, 0, len(blk.items))
		for i, it := range blk.items {
			row := mdNew("Row").(*Row)
			row.CrossAlign.Set("start")
			if spaced || i > 0 {
				row.marginTop.Set(4)
			}
			if it.depth > 0 {
				row.paddingLeft.Set(float64(it.depth) * 20)
			}
			marker := "•"
			if it.ordered {
				marker = strconv.Itoa(it.number) + "."
			}
			bullet := mdNew("Text").(*Text)
			bullet.Text.Set(marker)
			bullet.width.Set(18)
			body := mdNew("RichText").(*RichText)
			body.SetSpans(it.spans)
			body.Wrap.Set(true)
			body.flex.Set(1)
			for _, k := range []Node{bullet, body} {
				kb := k.Base()
				kb.node = k
				kb.parent = &row.NodeBase
				kb.tree = c.tree
				kb.scope = c.scope
				kb.hooks = c.hooks
				row.kids = append(row.kids, kb)
			}
			out = append(out, row)
		}
		return out
	case mdQuote:
		col := mdNew("Column").(*Column)
		// The tree link has to exist before a role is written: the resolver
		// finds its nodes through the tree's list.
		col.tree = c.tree
		col.borderLeftW.Set(3)
		col.writeRole("borderLeftColor", "outlineVariant")
		col.paddingLeft.Set(12)
		if spaced {
			col.marginTop.Set(mdBlockGap)
		}
		for _, k := range mdNodes(c, blk.children) {
			kb := k.Base()
			kb.node = k
			kb.parent = &col.NodeBase
			kb.tree = c.tree
			kb.scope = c.scope
			kb.hooks = c.hooks
			col.kids = append(col.kids, kb)
		}
		return []Node{col}
	case mdTableBlock:
		t := mdNew("Table").(*Table)
		if spaced {
			t.marginTop.Set(mdBlockGap)
		}
		t.alignSelf.Set("stretch")
		for i, head := range blk.header {
			col := mdNew("TableColumn").(*TableColumn)
			col.Label.Set(head)
			if i < len(blk.aligns) && blk.aligns[i] != "" {
				col.Align.Set(blk.aligns[i])
			}
			link(c, &t.NodeBase, col)
		}
		for _, row := range blk.rows {
			r := mdNew("TableRow").(*TableRow)
			for i, cell := range row {
				cellNode := mdNew("TableCell").(*TableCell)
				// Value is the machine-readable string the table sorts on; the
				// reader sees a RichText child, the way a designed cell works.
				cellNode.Value.Set(cell.text)
				text := mdNew("RichText").(*RichText)
				text.SetSpans(cell.spans)
				text.Wrap.Set(true)
				if i < len(blk.aligns) && blk.aligns[i] != "" {
					cellNode.Align.Set(blk.aligns[i])
					text.Align.Set(blk.aligns[i])
				}
				link(c, &cellNode.NodeBase, text)
				link(c, &r.NodeBase, cellNode)
			}
			link(c, &t.NodeBase, r)
		}
		return []Node{t}
	}
	return nil
}

// mdNew creates one generated element the way the builder does: with the
// properties' defaults applied, so `visible` and the rest of the common set are
// what the rest of the framework expects.
func mdNew(kind string) Node {
	n := newNode(kind)
	if n == nil {
		return nil
	}
	b := n.Base()
	b.node = n
	if def, ok := schema.Lookup(kind); ok {
		b.properties()
		applyDefaults(b, def)
	}
	return n
}

// link wires a generated node into its parent, which is what layout and paint
// walk.
func link(c *Markdown, parent *NodeBase, n Node) {
	b := n.Base()
	b.node = n
	b.parent = parent
	b.tree = c.tree
	b.scope = c.scope
	b.hooks = c.hooks
	parent.kids = append(parent.kids, b)
}

// mdKind is what a parsed block turns into.
type mdKind uint8

const (
	mdParagraph mdKind = iota
	mdHeading
	mdCode
	mdRule
	mdList
	mdQuote
	mdTableBlock
)

// mdBlock is one parsed block. text is the plain string a property takes;
// spans are the styled runs a paragraph draws.
type mdBlock struct {
	kind     mdKind
	level    int // heading level
	text     string
	spans    []Span
	lang     string
	items    []mdItem
	children []mdBlock

	header []string
	rows   [][]mdCell
	aligns []string
}

// mdCell is one table cell: the plain value the table sorts on, and the runs it
// draws.
type mdCell struct {
	text  string
	spans []Span
}

// mdItem is one list entry. depth is how many lists deep it sits, which paint
// turns into an indent.
type mdItem struct {
	depth   int
	spans   []Span
	ordered bool
	number  int
}

// mdParser is the whole parser: GitHub-flavoured markdown, so tables, task
// lists, strikethrough and linkify all arrive as nodes.
var mdParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

// parseMarkdown turns a document into blocks. Anything it does not model (raw
// HTML, footnotes) is skipped rather than drawn as source.
func parseMarkdown(src string) []mdBlock {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	doc := mdParser.Parse(text.NewReader([]byte(src)))
	return mdBlocks(doc, []byte(src))
}

// mdBlocks collects a node's children as blocks.
func mdBlocks(n ast.Node, src []byte) []mdBlock {
	var out []mdBlock
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch x := c.(type) {
		case *ast.Heading:
			spans := mdSpans(c, src)
			out = append(out, mdBlock{kind: mdHeading, level: x.Level, spans: spans, text: mdSpanText(spans)})
		case *ast.Paragraph:
			if spans := mdSpans(c, src); len(spans) > 0 {
				out = append(out, mdBlock{kind: mdParagraph, spans: spans, text: mdSpanText(spans)})
			}
		case *ast.TextBlock:
			// A tight list item holds its text in a TextBlock; it only reaches
			// here for a looser shape than mdItems handles.
			if spans := mdSpans(c, src); len(spans) > 0 {
				out = append(out, mdBlock{kind: mdParagraph, spans: spans, text: mdSpanText(spans)})
			}
		case *ast.FencedCodeBlock:
			out = append(out, mdBlock{kind: mdCode, lang: string(x.Language(src)), text: string(x.Lines().Value(src))})
		case *ast.CodeBlock:
			out = append(out, mdBlock{kind: mdCode, text: string(x.Lines().Value(src))})
		case *ast.Blockquote:
			out = append(out, mdBlock{kind: mdQuote, children: mdBlocks(c, src)})
		case *ast.List:
			var items []mdItem
			mdItems(x, src, 0, &items)
			if len(items) > 0 {
				out = append(out, mdBlock{kind: mdList, items: items})
			}
		case *ast.ThematicBreak:
			out = append(out, mdBlock{kind: mdRule})
		case *extast.Table:
			if t, ok := parseTable(x, src); ok {
				out = append(out, t)
			}
		}
	}
	return out
}

// mdItems flattens a list, including its nested lists, into entries whose depth
// is the nesting level.
func mdItems(list *ast.List, src []byte, depth int, out *[]mdItem) {
	num := 1
	if list.IsOrdered() && list.Start > 0 {
		num = list.Start
	}
	for it := list.FirstChild(); it != nil; it = it.NextSibling() {
		li, ok := it.(*ast.ListItem)
		if !ok {
			continue
		}
		item := mdItem{depth: depth, ordered: list.IsOrdered(), number: num}
		var nested []mdItem
		for sub := li.FirstChild(); sub != nil; sub = sub.NextSibling() {
			switch s := sub.(type) {
			case *ast.TextBlock, *ast.Paragraph:
				if spans := mdSpans(sub, src); len(spans) > 0 {
					if len(item.spans) > 0 {
						item.spans = append(item.spans, Span{Text: " "})
					}
					item.spans = append(item.spans, spans...)
				}
			case *ast.List:
				mdItems(s, src, depth+1, &nested)
			case *extast.TaskCheckBox:
				if s.IsChecked {
					item.spans = append(item.spans, Span{Text: "☑ "})
				} else {
					item.spans = append(item.spans, Span{Text: "☐ "})
				}
			case *ast.FencedCodeBlock:
				if spans := mdSpans(sub, src); len(spans) > 0 {
					item.spans = append(item.spans, spans...)
				}
			}
		}
		if len(item.spans) > 0 {
			*out = append(*out, item)
		}
		*out = append(*out, nested...)
		num++
	}
}

// parseTable reads a GFM table: its header, its rows and the column alignments.
func parseTable(t *extast.Table, src []byte) (mdBlock, bool) {
	blk := mdBlock{kind: mdTableBlock}
	for _, a := range t.Alignments {
		switch a {
		case extast.AlignCenter:
			blk.aligns = append(blk.aligns, "center")
		case extast.AlignRight:
			blk.aligns = append(blk.aligns, "end")
		default:
			blk.aligns = append(blk.aligns, "start")
		}
	}
	for n := t.FirstChild(); n != nil; n = n.NextSibling() {
		var cells []mdCell
		for cell := n.FirstChild(); cell != nil; cell = cell.NextSibling() {
			spans := mdSpans(cell, src)
			cells = append(cells, mdCell{text: mdSpanText(spans), spans: spans})
		}
		if _, ok := n.(*extast.TableHeader); ok {
			// A header cell is drawn by the column's label, which is one plain
			// string, so the header keeps only its words.
			for _, c := range cells {
				blk.header = append(blk.header, c.text)
			}
			continue
		}
		blk.rows = append(blk.rows, cells)
	}
	return blk, len(blk.header) > 0
}

// mdSpans turns a node's inline markup into styled runs. A run keeps its
// weight, slant, tint, strike and link, so one paragraph can mix all of them.
func mdSpans(n ast.Node, src []byte) []Span {
	var out []Span
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var s Span
		switch x := c.(type) {
		case *ast.Text:
			s.Text = string(x.Segment.Value(src))
			if x.SoftLineBreak() || x.HardLineBreak() {
				s.Text += " "
			}
		case *ast.String:
			s.Text = string(x.Value)
		case *ast.AutoLink:
			s.Text, s.Link = string(x.Label(src)), string(x.URL(src))
		default:
			return ast.WalkContinue, nil
		}
		s.Bold, s.Italic, s.Code, s.Strike, s.Link = mdSwitches(c, s.Link)
		out = mdAppend(out, s)
		return ast.WalkContinue, nil
	})
	return mdTrim(out)
}

// mdSwitches reads the styles an inline run inherits from its ancestors.
func mdSwitches(n ast.Node, link string) (bold, italic, code, strike bool, dest string) {
	dest = link
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch x := p.(type) {
		case *ast.Emphasis:
			// Strong is emphasis of level 2; a nested pair is both.
			if x.Level >= 2 {
				bold = true
			} else {
				italic = true
			}
		case *ast.CodeSpan:
			code = true
		case *extast.Strikethrough:
			strike = true
		case *ast.Link:
			if dest == "" {
				dest = string(x.Destination)
			}
		}
	}
	return bold, italic, code, strike, dest
}

// mdAppend adds a run, merging it into the previous one when the style matches,
// which keeps a word split by the parser as one drawn run.
func mdAppend(out []Span, s Span) []Span {
	if s.Text == "" {
		return out
	}
	if n := len(out); n > 0 && mdSameStyle(out[n-1], s) {
		out[n-1].Text += s.Text
		return out
	}
	return append(out, s)
}

// mdSameStyle compares two runs ignoring their text.
func mdSameStyle(a, b Span) bool {
	a.Text, b.Text = "", ""
	return a == b
}

// mdTrim drops the whitespace a block starts and ends with, the way the plain
// flattening did.
func mdTrim(spans []Span) []Span {
	for len(spans) > 0 {
		spans[0].Text = strings.TrimLeft(spans[0].Text, " \t\n")
		if spans[0].Text != "" {
			break
		}
		spans = spans[1:]
	}
	for len(spans) > 0 {
		last := len(spans) - 1
		spans[last].Text = strings.TrimRight(spans[last].Text, " \t\n")
		if spans[last].Text != "" {
			break
		}
		spans = spans[:last]
	}
	return spans
}

// mdSpanText flattens runs back to the plain string a property or a table's
// sort value needs.
func mdSpanText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}
