// Package goifmt formats a .goi design the way gofmt formats Go source.
//
// The layout is deterministic, so running it twice changes nothing: the file's
// own whitespace is not preserved, only its comments and their cuddling.
//
//   - imports, props, bridge and type blocks are separated by one blank line
//   - a declaration's names and types are aligned in one column
//   - an element body puts one property per line, with the values aligned
//   - a property run and the children after it are separated by one blank line,
//     and so are two children; the closing brace follows the last member with no
//     blank line
//   - an element with no members is one token: `Text {}`
package goifmt

import (
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
)

// Source formats src. A file that does not parse comes back unchanged, so a
// half-typed save never destroys the layout.
func Source(src []byte) []byte {
	f, diag := syntax.Parse(src)
	if !diag.Empty() || f == nil || f.Root == nil {
		return src
	}
	p := &printer{src: src, file: f, end: syntax.Pos(len(src))}
	p.document()
	return []byte(p.b.String())
}

// File formats an already parsed file. src is the text those positions point
// into.
func File(f *syntax.File, src []byte) []byte {
	if f == nil || f.Root == nil {
		return src
	}
	p := &printer{src: src, file: f, end: syntax.Pos(len(src))}
	p.document()
	return []byte(p.b.String())
}

type printer struct {
	src  []byte
	file *syntax.File
	b    strings.Builder
	ci   int // next comment to place, into file.Comments
	// end is one past the source, the bound for a comment after the last token.
	end   syntax.Pos
	depth int
}

// ------------------------------------------------------------------ document

func (p *printer) document() {
	first := p.file.Root.NameAt
	if len(p.file.Imports) > 0 {
		first = p.file.Imports[0].At
	}
	if len(p.file.Props) > 0 && p.file.PropsAt < first {
		first = p.file.PropsAt
	}
	if len(p.file.Vars) > 0 && p.file.BridgeAt < first {
		first = p.file.BridgeAt
	}
	if len(p.file.Shapes) > 0 && p.file.Shapes[0].NameAt < first {
		first = p.file.Shapes[0].NameAt
	}
	p.headerComments(first)

	wrote := false
	section := func(fn func()) {
		if wrote {
			p.b.WriteString("\n")
		}
		fn()
		wrote = true
	}
	if len(p.file.Imports) > 0 {
		section(func() {
			for _, imp := range p.file.Imports {
				p.commentsBefore(imp.At)
				p.b.WriteString("import " + imp.Alias + " " + strconv.Quote(imp.Path) + "\n")
			}
		})
	}
	if len(p.file.Props) > 0 {
		section(func() {
			p.commentsBefore(p.file.PropsAt)
			p.declBlock("props", p.file.Props, p.file.Props[len(p.file.Props)-1].At)
		})
	}
	if len(p.file.Vars) > 0 {
		section(func() {
			p.commentsBefore(p.file.BridgeAt)
			p.declBlock("bridge", p.file.Vars, p.file.Vars[len(p.file.Vars)-1].At)
		})
	}
	for _, sh := range p.file.Shapes {
		sh := sh
		section(func() {
			p.commentsBefore(sh.NameAt)
			p.b.WriteString("type " + sh.Name + " {\n")
			p.depth++
			p.fieldLines(sh.Fields)
			p.depth--
			p.b.WriteString("}\n")
		})
	}
	if wrote {
		p.b.WriteString("\n")
	}
	p.element(p.file.Root, p.end)
}

// headerComments writes the comments before the first declaration, then one
// blank line, so the file's own header stays a header.
func (p *printer) headerComments(first syntax.Pos) {
	p.commentsBefore(first)
	if p.ci > 0 {
		p.b.WriteString("\n")
	}
}

// --------------------------------------------------------------- declarations

// declBlock writes `props { … }` or `bridge { … }` with aligned fields.
func (p *printer) declBlock(head string, fields []syntax.Field, last syntax.Pos) {
	p.b.WriteString(head + " {\n")
	p.depth++
	p.fieldLines(fields)
	p.depth--
	p.commentsTo(last)
	p.b.WriteString("}\n")
}

// fieldLines writes one declaration per line, names padded so the types line up
// in a column, the way gofmt aligns a struct.
func (p *printer) fieldLines(fields []syntax.Field) {
	width := 0
	for _, f := range fields {
		if n := len(f.Name); n > width {
			width = n
		}
	}
	for i, f := range fields {
		stop := p.end
		if i+1 < len(fields) {
			stop = fields[i+1].NameAt
		}
		p.commentsBefore(f.NameAt)
		p.indent()
		p.b.WriteString(f.Name)
		p.b.WriteString(strings.Repeat(" ", width-len(f.Name)+1))
		p.b.WriteString(f.Type.String())
		p.b.WriteString("\n")
		p.trailing(stop)
	}
}

// -------------------------------------------------------------------- elements

func (p *printer) element(e *syntax.Element, stop syntax.Pos) {
	p.commentsBefore(e.NameAt)
	p.indent()
	p.b.WriteString(e.Name)
	if len(e.Members) == 0 {
		p.b.WriteString(" {}\n")
		p.commentsTo(e.Close)
		p.trailing(stop)
		return
	}
	p.b.WriteString(" {\n")
	p.trailing(memberPos(e.Members[0]))
	p.depth++
	p.members(e.Members, e.Close)
	p.commentsTo(e.Close)
	p.depth--
	p.indent()
	p.b.WriteString("}\n")
	p.trailing(stop)
}

// members lays out one body: properties first, then children, with a blank line
// between the two groups and between two children.
func (p *printer) members(members []syntax.Member, close syntax.Pos) {
	var props []*syntax.Property
	var kids []syntax.Member
	for _, m := range members {
		if pr, ok := m.(*syntax.Property); ok {
			props = append(props, pr)
			continue
		}
		kids = append(kids, m)
	}

	// A property run: one per line, the values in one column.
	width := 0
	for _, pr := range props {
		if n := len(pr.Name) + 1; n > width {
			width = n
		}
	}
	for i, pr := range props {
		stop := close
		switch {
		case i+1 < len(props):
			stop = props[i+1].NameAt
		case len(kids) > 0:
			stop = memberPos(kids[0])
		}
		p.commentsBefore(pr.NameAt)
		p.indent()
		p.b.WriteString(pr.Name + ":")
		p.b.WriteString(strings.Repeat(" ", width-len(pr.Name)))
		p.b.WriteString(printProperty(pr.Name, pr.Value, p.depth))
		p.b.WriteString("\n")
		p.trailing(stop)
	}

	if len(props) > 0 && len(kids) > 0 {
		p.b.WriteString("\n")
	}
	for i, m := range kids {
		if i > 0 {
			p.b.WriteString("\n")
		}
		stop := close
		if i+1 < len(kids) {
			stop = memberPos(kids[i+1])
		}
		switch x := m.(type) {
		case *syntax.Element:
			p.element(x, stop)
		case *syntax.Block:
			p.block(x, stop)
		}
	}
}

func (p *printer) block(b *syntax.Block, stop syntax.Pos) {
	if b.Kind == syntax.BlockFor {
		p.commentsBefore(b.At)
		p.indent()
		p.b.WriteString("for ")
		switch {
		case b.KeyVar != "" && b.ValVar != "":
			p.b.WriteString(b.KeyVar + ", " + b.ValVar + " := ")
		case b.KeyVar != "":
			p.b.WriteString(b.KeyVar + " := ")
		}
		p.b.WriteString("range " + printValue(b.Range) + " {\n")
		if len(b.Branches) > 0 && len(b.Branches[0].Members) > 0 {
			p.trailing(memberPos(b.Branches[0].Members[0]))
		}
		p.depth++
		if len(b.Branches) > 0 {
			p.members(b.Branches[0].Members, b.Close)
		}
		p.commentsTo(b.Close)
		p.depth--
		p.indent()
		p.b.WriteString("}\n")
		p.trailing(stop)
		return
	}
	if b.Kind == syntax.BlockSwitch {
		p.commentsBefore(b.At)
		p.indent()
		p.b.WriteString("switch " + printValue(b.Subject) + " {\n")
		if len(b.Branches) > 0 {
			p.trailing(b.Branches[0].At)
		}
		for i, br := range b.Branches {
			p.commentsBefore(br.At)
			p.indent()
			if len(br.Vals) == 0 {
				p.b.WriteString("default:\n")
			} else {
				vals := make([]string, len(br.Vals))
				for i, v := range br.Vals {
					vals[i] = printValue(v)
				}
				p.b.WriteString("case " + strings.Join(vals, ", ") + ":\n")
			}
			if len(br.Members) > 0 {
				p.trailing(memberPos(br.Members[0]))
			}
			p.depth++
			p.members(br.Members, b.Close)
			if i == len(b.Branches)-1 {
				p.commentsTo(b.Close)
			}
			p.depth--
		}
		p.indent()
		p.b.WriteString("}\n")
		p.trailing(stop)
		return
	}
	for i, br := range b.Branches {
		p.indent()
		switch {
		case i == 0:
			p.b.WriteString("if " + printValue(br.Cond) + " {\n")
		case br.Cond != nil:
			p.b.WriteString("} else if " + printValue(br.Cond) + " {\n")
		default:
			p.b.WriteString("} else {\n")
		}
		if len(br.Members) > 0 {
			p.trailing(memberPos(br.Members[0]))
		}
		p.depth++
		p.members(br.Members, b.Close)
		if i == len(b.Branches)-1 {
			p.commentsTo(b.Close)
		}
		p.depth--
	}
	p.indent()
	p.b.WriteString("}\n")
	p.trailing(stop)
}

// -------------------------------------------------------------------- comments

func (p *printer) indent() {
	p.b.WriteString(strings.Repeat("\t", p.depth))
}

// commentsBefore writes the comments that start before pos, each on its own
// line. A comment on the line directly above what follows stays cuddled; a gap
// keeps a blank line.
func (p *printer) commentsBefore(pos syntax.Pos) []syntax.Comment {
	var out []syntax.Comment
	for p.ci < len(p.file.Comments) && p.file.Comments[p.ci].At < pos {
		c := p.file.Comments[p.ci]
		p.ci++
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		out = append(out, c)
		p.writeComment(c)
		p.b.WriteString("\n")
	}
	return out
}

// commentsTo writes the remaining comments up to pos, which is how a comment
// after the last member and before `}` is kept.
func (p *printer) commentsTo(pos syntax.Pos) {
	for p.ci < len(p.file.Comments) && p.file.Comments[p.ci].At < pos {
		c := p.file.Comments[p.ci]
		p.ci++
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		p.writeComment(c)
		p.b.WriteString("\n")
	}
}

// trailing appends a comment that sat on the same line as the text just
// written. stop is where the next member begins: a comment past it belongs to
// that member, even when the whole element was written on one line.
func (p *printer) trailing(stop syntax.Pos) {
	for p.ci < len(p.file.Comments) {
		c := p.file.Comments[p.ci]
		if c.At >= stop || p.aloneOnLine(c.At) {
			return
		}
		p.ci++
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		// Cut the newline the member line already wrote.
		s := p.b.String()
		if strings.HasSuffix(s, "\n") {
			p.b.Reset()
			p.b.WriteString(strings.TrimSuffix(s, "\n"))
		}
		p.b.WriteString(" " + strings.TrimSpace(c.Text) + "\n")
	}
}

func (p *printer) writeComment(c syntax.Comment) {
	p.indent()
	p.b.WriteString(strings.TrimRight(c.Text, " \t"))
}

// aloneOnLine reports whether only whitespace precedes pos on its source line,
// which makes the comment there a comment above what follows rather than a
// trailing one.
func (p *printer) aloneOnLine(pos syntax.Pos) bool {
	for i := int(pos) - 1; i >= 0; i-- {
		switch p.src[i] {
		case '\n':
			return true
		case ' ', '\t', '\r':
		default:
			return false
		}
	}
	return true
}

// memberPos is where a member starts, which is what a comment before it has to
// respect.
func memberPos(m syntax.Member) syntax.Pos {
	switch x := m.(type) {
	case *syntax.Property:
		return x.NameAt
	case *syntax.Element:
		return x.NameAt
	case *syntax.Block:
		return x.At
	}
	return 0
}
