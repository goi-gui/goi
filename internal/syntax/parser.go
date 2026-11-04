package syntax

import "strings"

// Parse parses src into a File. It always returns as many diagnostics as it can
// find; File is nil when nothing parseable was produced (no root element).
func Parse(src []byte) (*File, Diagnostics) {
	lx := newLexer(src)
	toks, diag := lx.run()
	p := &parser{toks: toks, diag: diag}
	f := p.parseFile()
	if f == nil {
		return nil, p.diag
	}
	return f, p.diag
}

// ParseNamed is Parse with a source path, so diagnostics and imports can name
// the file they came from.
func ParseNamed(src []byte, name string) (*File, Diagnostics) {
	f, diag := Parse(src)
	if f != nil {
		f.Src = name
		markSrc(f.Root, name)
	}
	return f, diag
}

// markSrc records the source file on every element, so a diagnostic can point
// at the right file after imports are expanded.
func markSrc(e *Element, src string) {
	if e == nil {
		return
	}
	e.Src = src
	for i := range e.Members {
		switch m := e.Members[i].(type) {
		case *Property:
			m.Src = src
		case *Element:
			markSrc(m, src)
		case *Block:
			m.Src = src
			for j := range m.Branches {
				markSrcIn(m.Branches[j].Members, src)
			}
		}
	}
}

// markSrcIn records the source file on a block arm's members.
func markSrcIn(members []Member, src string) {
	for i := range members {
		switch m := members[i].(type) {
		case *Property:
			m.Src = src
		case *Element:
			markSrc(m, src)
		case *Block:
			m.Src = src
			for j := range m.Branches {
				markSrcIn(m.Branches[j].Members, src)
			}
		}
	}
}

type parser struct {
	toks []token
	i    int
	diag Diagnostics
	// slot records a `$children` marker, which makes the file a fragment.
	slot bool
}

func (p *parser) peek() token  { return p.toks[min(p.i, len(p.toks)-1)] }
func (p *parser) peek2() token { return p.toks[min(p.i+1, len(p.toks)-1)] }

// next never advances past the trailing EOF token, so error recovery can call
// it freely without running off the slice.
func (p *parser) next() token {
	t := p.peek()
	if t.kind != tokEOF {
		p.i++
	}
	return t
}

func (p *parser) errorf(t token, msg string) {
	p.diag = append(p.diag, Diagnostic{Pos: t.pos, End: t.end, Message: msg})
}

// sync skips tokens until a plausible member or element boundary so one bad
// literal does not swallow the rest of the file.
func (p *parser) sync() {
	depth := 0
	for {
		switch p.peek().kind {
		case tokEOF:
			return
		case tokLBrace:
			depth++
		case tokRBrace:
			if depth == 0 {
				return
			}
			depth--
		case tokSemicolon:
			if depth == 0 {
				return
			}
		case tokIdent:
			if depth == 0 && p.peek2().kind == tokColon {
				return
			}
		}
		p.next()
	}
}

func (p *parser) parseFile() *File {
	f := &File{}
	// The preamble: `import ... as ...`, the `props` block, `bridge` and `type`
	// may appear in any order before the root element.
	for {
		t := p.peek()
		if t.kind != tokIdent {
			break
		}
		switch {
		case t.text == "import":
			// Report a malformed import here rather than treating it as an
			// element named "import".
			imp, ok := p.parseImport()
			if ok {
				f.Imports = append(f.Imports, imp)
			}
		case t.text == "props":
			p.parseProps(f)
		case t.text == "data", t.text == "bridge":
			// `data` is the original name; `bridge` says what the block is:
			// the contract between Go and the design. Both parse alike.
			p.parseData(f)
		case t.text == "type":
			p.parseData(f)
		default:
			// An element that happens to start the file.
			root := p.parseElement()
			if root == nil {
				return nil
			}
			f.Root = root
			f.Fragment = p.slot
			p.parseTrailer()
			return f
		}
	}
	root := p.parseElement()
	if root == nil {
		return nil
	}
	f.Root = root
	f.Fragment = p.slot
	p.parseTrailer()
	return f
}

// parseTrailer rejects content after the root element, one diagnostic each.
func (p *parser) parseTrailer() {
	for p.peek().kind != tokEOF {
		t := p.peek()
		if t.kind == tokRBrace {
			p.errorf(t, "unexpected '}'")
			p.next()
			continue
		}
		p.errorf(t, "unexpected content after the root element")
		if p.parseElement() == nil {
			p.next()
		}
	}
}

// parseImport reads `import "path" as Alias`.
func (p *parser) parseImport() (Import, bool) {
	kw := p.next() // import
	imp := Import{At: kw.pos}
	t := p.peek()
	if t.kind != tokString {
		p.errorf(t, "expected a quoted path after import")
		p.sync()
		return imp, false
	}
	imp.Path = p.next().text
	as := p.peek()
	if as.kind != tokIdent || as.text != "as" {
		p.errorf(as, `expected "as" after the import path`)
		p.sync()
		return imp, false
	}
	p.next()
	alias := p.peek()
	if alias.kind != tokIdent {
		p.errorf(alias, "expected an alias after \"as\"")
		p.sync()
		return imp, false
	}
	p.next()
	// Newlines are not tokens, so `import "x" as` followed by the root element
	// would silently take the element name as the alias. An alias is always
	// followed by another preamble line or the root, never by '{'.
	if p.peek().kind == tokLBrace {
		p.errorf(alias, "expected an alias after \"as\"")
		return imp, false
	}
	imp.Alias, imp.AliasAt = alias.text, alias.pos
	return imp, true
}

// parseProps reads a fragment's input block, the typed values an instance
// passes and the fragment reads as `props.name`:
//
//	props {
//		id: string
//		title: string = "untitled"
//	}
func (p *parser) parseProps(f *File) {
	p.next() // props
	if p.peek().kind != tokLBrace {
		p.errorf(p.peek(), "expected '{' after props")
		p.sync()
		return
	}
	p.next()
	for {
		t := p.peek()
		switch t.kind {
		case tokEOF:
			p.errorf(t, "unexpected end of file: missing '}'")
			return
		case tokRBrace:
			p.next()
			return
		case tokComma, tokSemicolon:
			p.next()
		case tokIdent:
			pr, ok := p.parseProp()
			if !ok {
				return
			}
			f.Props = append(f.Props, pr)
		default:
			p.errorf(t, "expected a prop declaration")
			if !p.recoverMember() {
				p.next()
			}
		}
	}
}

// parseProp reads one entry of a fragment's `props` block. It shares the type
// grammar with a bridge declaration, so a prop may be a scalar, a list of a
// shape, or a handler; a scalar may carry a default.
func (p *parser) parseProp() (Prop, bool) {
	fl, ok := p.parseDataField()
	if !ok {
		return Prop{}, false
	}
	return Prop{
		Name: fl.Name, NameAt: fl.NameAt, Type: fl.Type,
		List: fl.List, Elem: fl.Elem, Fn: fl.Fn, Args: fl.Args,
		Default: fl.Default, At: fl.At,
	}, true
}

// parseTemplate splits a backtick string into literal parts and the values
// inside its `${...}` holes. Each hole is parsed as a normal expression, so
// `\`${bridge.count + 1}\“ works like any other value.
func (p *parser) parseTemplate(t token) (Value, bool) {
	body := t.text
	base := int(t.pos) + 1 // the body starts right after the opening backtick
	tmpl := &Template{At: t.pos}
	lit := 0
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' {
			i++
			continue
		}
		if body[i] != '$' || i+1 >= len(body) || body[i+1] != '{' {
			continue
		}
		if i > lit {
			tmpl.Parts = append(tmpl.Parts, &String{At: Pos(base + lit), Val: body[lit:i]})
		}
		end, ok := matchBrace(body, i+1)
		if !ok {
			p.errorf(t, "unterminated ${ in a template string")
			return nil, false
		}
		v, ok := p.subValue(body[i+2:end], base+i+2)
		if !ok {
			return nil, false
		}
		tmpl.Parts = append(tmpl.Parts, v)
		i = end
		lit = i + 1
	}
	if lit < len(body) {
		tmpl.Parts = append(tmpl.Parts, &String{At: Pos(base + lit), Val: body[lit:]})
	}
	return tmpl, true
}

// matchBrace returns the index of the '}' that closes the '{' at open, skipping
// quoted text so a `${a == "}"}` is not cut short.
func matchBrace(body string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(body); i++ {
		switch body[i] {
		case '\'', '"', '`':
			q := body[i]
			for i++; i < len(body); i++ {
				if body[i] == '\\' {
					i++
					continue
				}
				if body[i] == q {
					break
				}
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// subValue parses the text of one `${...}` hole. The text is padded with spaces
// to the hole's byte offset, so every position it produces is already an offset
// into the real file and diagnostics land on the right column.
func (p *parser) subValue(src string, at int) (Value, bool) {
	lx := newLexer([]byte(strings.Repeat(" ", at) + src))
	toks, diag := lx.run()
	sp := &parser{toks: toks, diag: diag}
	v, ok := sp.parseValue()
	if ok && sp.peek().kind != tokEOF {
		sp.errorf(sp.peek(), "unexpected text after the interpolated expression")
		ok = false
	}
	p.diag = append(p.diag, sp.diag...)
	if !ok {
		return nil, false
	}
	return v, true
}

// parseData reads either the variables block (`bridge { loading: bool }`, also
// spelled `data { ... }`) or one row shape (`type ticket { title: string }`,
// also spelled `data ticket { ... }`).
func (p *parser) parseData(f *File) {
	kw := p.next() // "data", "bridge" or "type"
	name := p.peek()
	if name.kind == tokLBrace {
		open := p.next()
		for {
			t := p.peek()
			switch t.kind {
			case tokEOF:
				p.errorf(t, "unexpected end of file: missing '}'")
				return
			case tokRBrace:
				p.next()
				return
			case tokComma, tokSemicolon:
				p.next()
			case tokIdent:
				v, ok := p.parseDataField()
				if !ok {
					return
				}
				f.Vars = append(f.Vars, DataVar(v))
			default:
				_ = open
				p.errorf(t, "expected a variable declaration")
				if !p.recoverMember() {
					p.next()
				}
			}
		}
	}
	if name.kind != tokIdent {
		p.errorf(name, "expected '{' or a shape name after '"+kw.text+"'")
		p.sync()
		return
	}
	p.next()
	if p.peek().kind != tokLBrace {
		p.errorf(p.peek(), "expected '{' after the shape name")
		p.sync()
		return
	}
	sh := DataShape{Name: name.text, NameAt: name.pos}
	sh.At = p.next().pos
	for {
		t := p.peek()
		switch t.kind {
		case tokEOF:
			p.errorf(t, "unexpected end of file: missing '}'")
			return
		case tokRBrace:
			sh.Close = p.next().pos
			f.Shapes = append(f.Shapes, sh)
			return
		case tokComma, tokSemicolon:
			p.next()
		case tokIdent:
			fl, ok := p.parseDataField()
			if !ok {
				return
			}
			sh.Fields = append(sh.Fields, DataField(fl))
		default:
			p.errorf(t, "expected a field name")
			if !p.recoverMember() {
				p.next()
			}
		}
	}
}

// dataField is one `name: type` declaration, shared by variables and shapes.
type dataField struct {
	Name    string
	NameAt  Pos
	List    bool
	Type    string
	Elem    string
	Fn      bool
	Args    []DataField
	Default Value
	At      Pos
}

func (p *parser) parseDataField() (dataField, bool) {
	name := p.next()
	f := dataField{Name: name.text, NameAt: name.pos}
	if p.peek().kind != tokColon {
		p.errorf(p.peek(), "expected ':' after the name")
		p.sync()
		return f, false
	}
	p.next()
	t := p.peek()
	switch t.kind {
	case tokLBracket:
		// [shape] is the list form; shape[] below is the original spelling.
		p.next()
		elem := p.peek()
		if elem.kind != tokIdent {
			p.errorf(elem, "expected a shape name inside '['")
			p.sync()
			return f, false
		}
		p.next()
		if p.peek().kind != tokRBracket {
			p.errorf(p.peek(), "expected ']' after the shape name")
			p.sync()
			return f, false
		}
		p.next()
		f.List, f.Elem, f.Type, f.At = true, elem.text, "list", elem.pos
	case tokIdent:
		p.next()
		if t.text == "func" {
			if p.peek().kind != tokLParen {
				p.errorf(p.peek(), "expected '(' after func")
				p.sync()
				return f, false
			}
			p.next()
			args, ok := p.parseFuncArgs()
			if !ok {
				p.sync()
				return f, false
			}
			f.Fn, f.Type, f.At, f.Args = true, "func", t.pos, args
			break
		}
		f.Type, f.At = t.text, t.pos
		if p.peek().kind == tokLBracket {
			p.next()
			if p.peek().kind != tokRBracket {
				p.errorf(p.peek(), "expected ']' after '['")
				p.sync()
				return f, false
			}
			p.next()
			f.List, f.Elem, f.Type = true, t.text, "list"
		}
	default:
		p.errorf(t, "expected a type: bool, string, number, color, [shape], shape[] or func(...)")
		p.sync()
		return f, false
	}
	// `count: number = 0` is the value the design starts with; Go may still
	// overwrite it with SetData.
	if p.peek().kind == tokAssign {
		p.next()
		if f.List || f.Fn {
			p.errorf(p.peek(), "only a scalar variable takes a default value")
			p.sync()
			return f, false
		}
		v, ok := p.parseValue()
		if !ok {
			p.sync()
			return f, false
		}
		f.Default = v
	}
	if p.peek().kind == tokSemicolon {
		p.next()
	}
	return f, true
}

// parseFuncArgs reads `(id: string, count: number)`, the parameters of a bridge
// handler.
func (p *parser) parseFuncArgs() ([]DataField, bool) {
	var out []DataField
	for {
		if p.peek().kind == tokRParen {
			p.next()
			return out, true
		}
		name := p.peek()
		if name.kind != tokIdent {
			p.errorf(name, "expected a handler parameter name")
			return nil, false
		}
		p.next()
		if p.peek().kind != tokColon {
			p.errorf(p.peek(), "expected ':' after the parameter name")
			return nil, false
		}
		p.next()
		typ := p.peek()
		if typ.kind != tokIdent {
			p.errorf(typ, "expected a parameter type: bool, string, number or color")
			return nil, false
		}
		p.next()
		out = append(out, DataField{Name: name.text, NameAt: name.pos, Type: typ.text, At: typ.pos})
		switch p.peek().kind {
		case tokComma, tokSemicolon:
			p.next()
		case tokRParen:
			p.next()
			return out, true
		default:
			p.errorf(p.peek(), "expected ',' or ')' after a handler parameter")
			return nil, false
		}
	}
}

func (p *parser) parseElement() *Element {
	t := p.peek()
	if t.kind != tokIdent {
		if t.kind == tokEOF {
			p.errorf(t, "expected an element")
			return nil
		}
		p.errorf(t, "expected an element name")
		return nil
	}
	p.next()
	el := &Element{Name: t.text, NameAt: t.pos, Open: t.pos, Close: t.pos}
	if p.peek().kind != tokLBrace {
		p.errorf(p.peek(), "expected '{' after element name")
		p.sync()
		return nil
	}
	el.Open = p.next().pos

	ml := p.parseMembers(false, nil)
	el.Members = ml.members
	el.Close = ml.close
	return el
}

// memberList is one parsed run of members plus how it ended.
type memberList struct {
	members []Member
	// end describes the `{:else}`/`{/if}` that stopped a block body.
	end blockEnd
	// close is the position of the '}' that closed an element body.
	close Pos
	// closed is true when the element's '}' was consumed.
	closed bool
}

// parseMembers reads members until the element's '}' (inBlock false) or until a
// block terminator `{:else ...}` / `{/if}` (inBlock true).
func (p *parser) parseMembers(inBlock bool, open *Block) memberList {
	var out memberList
	for {
		t := p.peek()
		switch t.kind {
		case tokEOF:
			p.errorf(t, "unexpected end of file: missing '}'")
			out.close = t.pos
			return out
		case tokRBrace:
			if inBlock {
				p.errorf(t, "expected {:else} or {/if} before '}'")
			}
			out.close = p.next().pos
			out.closed = true
			return out
		case tokSemicolon, tokComma:
			p.next() // stray separators are harmless
		case tokLBrace:
			// Inside a block body, `{:` and `{/` end the arm instead of
			// starting something.
			if inBlock && (p.peek2().kind == tokColon || p.peek2().kind == tokSlash) {
				out.end = p.parseBlockEnd(open)
				return out
			}
			if blk := p.parseBlock(); blk != nil {
				out.members = append(out.members, blk)
			}
		case tokIdent:
			// `Ident :` is a property, `Ident {` is a child element.
			if p.peek2().kind == tokColon {
				prop, ok := p.parseProperty()
				if ok {
					out.members = append(out.members, prop)
				}
				continue
			}
			child := p.parseElement()
			if child != nil {
				out.members = append(out.members, child)
			}
		case tokDollar:
			if slot := p.parseSlot(); slot != nil {
				out.members = append(out.members, slot)
			}
		default:
			p.errorf(t, "expected a property or child element")
			if !p.recoverMember() {
				p.next()
			}
		}
	}
}

// blockEndKind is how a block arm ended.
type blockEndKind int

const (
	blockEndClose blockEndKind = iota + 1
	blockEndElse
	blockEndElseIf
)

// blockEnd is a parsed `{:else}`, `{:else if cond}` or `{/if}`.
type blockEnd struct {
	kind blockEndKind
	cond Value
	at   Pos
}

// parseBlockEnd reads the `{:else ...}` or `{/if}` that closes an arm.
func (p *parser) parseBlockEnd(openBlk *Block) blockEnd {
	open := p.next() // '{'
	sep := p.next()  // ':' or '/'
	name := p.peek()
	if name.kind != tokIdent {
		p.errorf(name, "expected a block name after '{:' or '{/'")
		p.sync()
		return blockEnd{}
	}
	p.next()

	want := "if"
	if openBlk != nil && openBlk.Kind == BlockEach {
		want = "each"
	}
	if sep.kind == tokSlash {
		if name.text != want {
			p.errorf(name, "unexpected {/"+name.text+"}: no {#"+name.text+"} is open")
		}
		if p.peek().kind == tokRBrace {
			p.next()
		} else {
			p.errorf(p.peek(), "expected '}' after {/"+name.text+"}")
		}
		return blockEnd{kind: blockEndClose, at: open.pos}
	}

	end := blockEnd{kind: blockEndElse, at: open.pos}
	if name.text != "else" {
		p.errorf(name, "expected 'else', got "+name.text)
		p.sync()
		return end
	}
	if want == "each" {
		// An each block has one empty arm; `{:else if}` would be a condition,
		// which a list does not have.
		if p.peek().kind != tokRBrace {
			p.errorf(p.peek(), "expected '}' after {:else}")
		} else {
			p.next()
		}
		return end
	}
	if p.peek().kind == tokIdent && p.peek().text == "if" {
		p.next()
		cond, ok := p.parseValue()
		if !ok {
			p.sync()
			return end
		}
		end.kind = blockEndElseIf
		end.cond = cond
	}
	if p.peek().kind != tokRBrace {
		p.errorf(p.peek(), "expected '}' after {:else}")
	} else {
		p.next()
	}
	return end
}

// parseBlock reads `{#if cond} ... {/if}` with its else arms. Nested blocks are
// ordinary members, so they recurse through parseMembers.
func (p *parser) parseBlock() *Block {
	open := p.next() // '{'
	hash := p.peek()
	if hash.kind != tokHash {
		p.errorf(hash, "expected '#' after '{': a block is {#if ...}")
		p.sync()
		return nil
	}
	p.next()
	name := p.peek()
	if name.kind != tokIdent {
		p.errorf(name, "expected a block name after '{#'")
		p.sync()
		return nil
	}
	p.next()
	blk := &Block{At: open.pos}
	switch name.text {
	case "if":
		blk.Kind = BlockIf
	case "each":
		blk.Kind = BlockEach
		src := p.peek()
		if src.kind != tokIdent {
			p.errorf(src, "expected a list variable after {#each}")
			p.sync()
			return nil
		}
		p.next()
		// The source is namespaced like any other app value: {#each bridge.rows as row}.
		if p.peek().kind != tokDot {
			p.errorf(p.peek(), "expected '.' after the bridge namespace: {#each bridge.rows as row}")
			p.sync()
			return nil
		}
		p.next()
		name := p.peek()
		if name.kind != tokIdent {
			p.errorf(name, "expected a list name after 'bridge.'")
			p.sync()
			return nil
		}
		p.next()
		switch src.text {
		case "data", "bridge":
		case "props":
			blk.SourceProps = true
		default:
			p.errorf(src, "a list comes from 'bridge.', 'data.' or 'props.'; "+src.text+"."+name.text+" is not a collection")
			p.sync()
			return nil
		}
		blk.Source, blk.SourceAt = name.text, name.pos
		// `as row` names one item inside the arm.
		kw := p.peek()
		if kw.kind != tokIdent || kw.text != "as" {
			p.errorf(kw, "expected 'as' after the {#each} list: {#each rows as row}")
			p.sync()
			return nil
		}
		p.next()
		v := p.peek()
		if v.kind != tokIdent {
			p.errorf(v, "expected a loop variable after 'as'")
			p.sync()
			return nil
		}
		p.next()
		blk.Var, blk.VarAt = v.text, v.pos
	default:
		p.errorf(name, "unknown block {#"+name.text+"}: only {#if} exists")
		p.sync()
		return nil
	}

	// An if block reads its condition here; an each block has none, because the
	// list picks the rows.
	var cond Value
	if blk.Kind == BlockIf {
		var ok bool
		cond, ok = p.parseValue()
		if !ok {
			p.sync()
			return nil
		}
	}
	if p.peek().kind != tokRBrace {
		p.errorf(p.peek(), "expected '}' after the block header")
		p.sync()
		return nil
	}
	p.next()

	arm := Branch{Cond: cond, At: open.pos}
	sawElse := false
	for {
		ml := p.parseMembers(true, blk)
		arm.Members = ml.members
		blk.Branches = append(blk.Branches, arm)
		if ml.end.kind == blockEndClose {
			blk.Close = ml.end.at
			return blk
		}
		switch ml.end.kind {
		case blockEndElse:
			if sawElse {
				p.errorf(p.peek(), "{:else} was already used")
			}
			sawElse = true
			arm = Branch{At: ml.end.at}
		case blockEndElseIf:
			if sawElse {
				p.errorf(p.peek(), "an {:else if} cannot follow {:else}")
			}
			arm = Branch{Cond: ml.end.cond, At: ml.end.at}
		default:
			// The file ended, or a '}' closed the arm's own element: either
			// way the block cannot continue.
			return blk
		}
	}
}

// parseSlot reads `$name`, the marker for where an instance's children go.
// `$children` is the unnamed slot; any other name is a named one. A bare marker
// is what a fragment body writes, and it makes the file a fragment. The same
// name with a body is how the caller passes elements to that slot.
func (p *parser) parseSlot() *Element {
	dollar := p.next()
	name := p.peek()
	if name.kind != tokIdent {
		p.errorf(name, "expected a name after '$'")
		return nil
	}
	p.next()
	el := &Element{Name: "$" + name.text, NameAt: dollar.pos, Open: dollar.pos, Close: name.end}
	if p.peek().kind != tokLBrace {
		p.slot = true
		return el
	}
	el.Open = p.next().pos
	ml := p.parseMembers(false, nil)
	el.Members = ml.members
	el.Close = ml.close
	return el
}

// recoverMember advances past one broken member and reports whether it made
// progress. It stops at a '}' so the enclosing element can still close.
func (p *parser) recoverMember() bool {
	start := p.i
	for p.peek().kind != tokEOF && p.peek().kind != tokRBrace {
		if p.peek().kind == tokSemicolon {
			p.next()
			break
		}
		p.next()
	}
	return p.i > start
}

func (p *parser) parseProperty() (*Property, bool) {
	name := p.next()
	prop := &Property{Name: name.text, NameAt: name.pos}
	p.next() // ':'

	v, ok := p.parseValue()
	if !ok {
		p.sync()
		return nil, false
	}
	prop.Value = v

	// Optional `;`. A newline also terminates a property, and newlines are not
	// tokens, so nothing to consume in that case.
	if p.peek().kind == tokSemicolon {
		p.next()
	}
	return prop, true
}

func (p *parser) parseValue() (Value, bool) {
	t := p.peek()
	switch t.kind {
	case tokString, tokTemplate:
		// A string is an operand too, so `"a" + "b"` concatenates as the
		// syntax reference promises.
		return p.parseExpr()
	case tokNumber, tokLParen, tokBang, tokMinus:
		return p.parseExpr()
	case tokBool:
		p.next()
		return &Bool{At: t.pos, Val: t.b}, true
	case tokColor:
		p.next()
		return &Color{At: t.pos, RGBA: t.rgba}, true
	case tokIdent:
		// `name` is an enum or id, `name.prop` is a reference.
		if p.peek2().kind == tokDot {
			return p.parseExpr()
		}
		p.next()
		return &Ident{At: t.pos, Val: t.text}, true
	case tokLBrace:
		return p.parseMap()
	case tokDollar:
		// A `$` starts an expression only for the diagnostic parseTerm gives;
		// going through the expression path keeps one message per '$'.
		return p.parseExpr()
	case tokError:
		// The lexer already reported this; stay quiet so the file does not
		// produce two diagnostics for one typo.
		return nil, false
	case tokRBrace, tokEOF:
		p.errorf(t, "expected a value")
		return nil, false
	default:
		p.errorf(t, "expected a value")
		return nil, false
	}
}

// parseExpr parses a full expression: a conditional at the top, then logical,
// comparison, additive and multiplicative levels.
func (p *parser) parseExpr() (Value, bool) {
	return p.parseTernary()
}

func (p *parser) parseTernary() (Value, bool) {
	cond, ok := p.parseOr()
	if !ok {
		return nil, false
	}
	if p.peek().kind != tokQuestion {
		return cond, true
	}
	at := p.next().pos
	then, ok := p.parseValue()
	if !ok {
		return nil, false
	}
	if p.peek().kind != tokColon {
		p.errorf(p.peek(), "expected ':' in a conditional expression")
		return nil, false
	}
	p.next()
	els, ok := p.parseValue()
	if !ok {
		return nil, false
	}
	return &Ternary{At: at, Cond: cond, Then: then, Else: els}, true
}

func (p *parser) parseOr() (Value, bool) {
	left, ok := p.parseAnd()
	if !ok {
		return nil, false
	}
	for p.peek().kind == tokOrOr {
		p.next()
		right, ok := p.parseAnd()
		if !ok {
			return nil, false
		}
		left = &Binary{At: left.Pos(), Op: "||", Left: left, Right: right}
	}
	return left, true
}

func (p *parser) parseAnd() (Value, bool) {
	left, ok := p.parseEquality()
	if !ok {
		return nil, false
	}
	for p.peek().kind == tokAndAnd {
		p.next()
		right, ok := p.parseEquality()
		if !ok {
			return nil, false
		}
		left = &Binary{At: left.Pos(), Op: "&&", Left: left, Right: right}
	}
	return left, true
}

func (p *parser) parseEquality() (Value, bool) {
	left, ok := p.parseComparison()
	if !ok {
		return nil, false
	}
	for {
		op := ""
		switch p.peek().kind {
		case tokEqEq:
			op = "=="
		case tokBangEq:
			op = "!="
		default:
			return left, true
		}
		p.next()
		right, ok := p.parseComparison()
		if !ok {
			return nil, false
		}
		left = &Binary{At: left.Pos(), Op: op, Left: left, Right: right}
	}
}

func (p *parser) parseComparison() (Value, bool) {
	left, ok := p.parseAdd()
	if !ok {
		return nil, false
	}
	for {
		op := ""
		switch p.peek().kind {
		case tokLt:
			op = "<"
		case tokLtEq:
			op = "<="
		case tokGt:
			op = ">"
		case tokGtEq:
			op = ">="
		default:
			return left, true
		}
		p.next()
		right, ok := p.parseAdd()
		if !ok {
			return nil, false
		}
		left = &Binary{At: left.Pos(), Op: op, Left: left, Right: right}
	}
}

func (p *parser) parseAdd() (Value, bool) {
	left, ok := p.parseMul()
	if !ok {
		return nil, false
	}
	for {
		op := ""
		switch p.peek().kind {
		case tokPlus:
			op = "+"
		case tokMinus:
			op = "-"
		default:
			return left, true
		}
		p.next()
		right, ok := p.parseMul()
		if !ok {
			return nil, false
		}
		left = &Binary{At: left.Pos(), Op: op, Left: left, Right: right}
	}
}

func (p *parser) parseMul() (Value, bool) {
	left, ok := p.parseTerm()
	if !ok {
		return nil, false
	}
	for {
		op := ""
		switch p.peek().kind {
		case tokStar:
			op = "*"
		case tokSlash:
			op = "/"
		default:
			return left, true
		}
		p.next()
		right, ok := p.parseTerm()
		if !ok {
			return nil, false
		}
		left = &Binary{At: left.Pos(), Op: op, Left: left, Right: right}
	}
}

func (p *parser) parseUnary() (Value, bool) {
	switch t := p.peek(); t.kind {
	case tokBang:
		p.next()
		x, ok := p.parseUnary()
		if !ok {
			return nil, false
		}
		return &Unary{At: t.pos, Op: "!", X: x}, true
	case tokMinus:
		p.next()
		x, ok := p.parseUnary()
		if !ok {
			return nil, false
		}
		return &Unary{At: t.pos, Op: "-", X: x}, true
	}
	return p.parseTerm()
}

// parseTerm parses one literal, reference, percentage or a parenthesised
// expression. A bare identifier is a reference when a '.' follows it and an
// identifier value otherwise, so `app.theme == dark` needs no quotes.
func (p *parser) parseTerm() (Value, bool) {
	t := p.peek()
	switch t.kind {
	case tokNumber:
		p.next()
		if p.peek().kind == tokPercent {
			p.next()
			return &Percent{At: t.pos, Val: t.num}, true
		}
		return &Number{At: t.pos, Val: t.num}, true
	case tokString:
		p.next()
		return &String{At: t.pos, Val: t.text}, true
	case tokTemplate:
		p.next()
		return p.parseTemplate(t)
	case tokBool:
		p.next()
		return &Bool{At: t.pos, Val: t.b}, true
	case tokColor:
		p.next()
		return &Color{At: t.pos, RGBA: t.rgba}, true
	case tokDollar:
		// `$name` marks a slot, and a slot is an element, not a value. A
		// fragment reads its inputs as `props.name`.
		t := p.next()
		p.errorf(t, "unexpected '$' in a value: a fragment reads its inputs as props.name")
		return nil, false
	case tokLParen:
		p.next()
		v, ok := p.parseExpr()
		if !ok {
			return nil, false
		}
		if p.peek().kind != tokRParen {
			p.errorf(p.peek(), "expected ')'")
			return nil, false
		}
		p.next()
		return v, true
	case tokIdent:
		id := p.next()
		if p.peek().kind != tokDot {
			return &Ident{At: id.pos, Val: id.text}, true
		}
		p.next()
		prop := p.peek()
		if prop.kind != tokIdent {
			p.errorf(prop, "expected a property name after '.'")
			return nil, false
		}
		p.next()
		ref := &Ref{At: id.pos, ID: id.text, Prop: prop.text}
		if p.peek().kind == tokLParen {
			return p.parseCall(ref)
		}
		return ref, true
	default:
		p.errorf(t, "expected a value or reference")
		return nil, false
	}
}

// parseCall reads `bridge.onOpen(post.id, "now")`, the value of an event
// property.
func (p *parser) parseCall(ref *Ref) (Value, bool) {
	p.next() // '('
	call := &Call{At: ref.At, Callee: ref}
	for p.peek().kind != tokRParen {
		v, ok := p.parseValue()
		if !ok {
			p.sync()
			return nil, false
		}
		call.Args = append(call.Args, v)
		switch p.peek().kind {
		case tokComma, tokSemicolon:
			p.next()
		case tokRParen:
		default:
			p.errorf(p.peek(), "expected ',' or ')' after a handler argument")
			return nil, false
		}
	}
	p.next() // ')'
	return call, true
}

// parseMap parses `{ key: value, ... }`, the responsive form of a value.
func (p *parser) parseMap() (Value, bool) {
	open := p.next()
	m := &Map{At: open.pos}
	for {
		t := p.peek()
		switch t.kind {
		case tokEOF:
			p.errorf(t, "unexpected end of file: missing '}' in value")
			return nil, false
		case tokRBrace:
			p.next()
			return m, true
		case tokComma, tokSemicolon:
			p.next()
		case tokIdent:
			key := p.next()
			if p.peek().kind != tokColon {
				p.errorf(p.peek(), "expected ':' after a map key")
				p.skipEntry()
				continue
			}
			p.next()
			v, ok := p.parseValue()
			if !ok {
				p.skipEntry()
				continue
			}
			m.Entries = append(m.Entries, MapEntry{Key: key.text, KeyAt: key.pos, Value: v})
		default:
			p.errorf(t, "expected a map key")
			p.skipEntry()
		}
	}
}

// skipEntry advances to the next map entry boundary after a broken one.
func (p *parser) skipEntry() {
	for {
		switch p.peek().kind {
		case tokEOF, tokRBrace, tokComma:
			return
		case tokLBrace:
			depth := 0
			for {
				t := p.next()
				if t.kind == tokEOF {
					return
				}
				if t.kind == tokLBrace {
					depth++
				}
				if t.kind == tokRBrace {
					depth--
					if depth == 0 {
						return
					}
				}
			}
		default:
			p.next()
		}
	}
}
