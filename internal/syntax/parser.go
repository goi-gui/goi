package syntax

import "bytes"

// Parse parses src into a File. It always returns as many diagnostics as it can
// find; File is nil when nothing parseable was produced (no root element).
func Parse(src []byte) (*File, Diagnostics) {
	lx := newLexer(src)
	toks, diag := lx.run()
	p := &parser{toks: toks, diag: diag, src: src}
	f := p.parseFile()
	if f == nil {
		return nil, p.diag
	}
	f.Comments = lx.comments
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
	src  []byte // the same source the lexer read, for line-aware literals
	toks []token
	i    int
	diag Diagnostics
	// slot records a `$children` marker, which makes the file a fragment.
	slot bool
	// caseDepth is non-zero inside a switch arm's member list, where `case` and
	// `default` end the arm instead of starting a member.
	caseDepth int
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
	// The preamble: `import Alias "path"`, the `props` block, `bridge` and `type`
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

// parseImport reads `import Sidebar "./Sidebar.goi"`, the order Go writes an
// import alias in.
func (p *parser) parseImport() (Import, bool) {
	kw := p.next() // import
	imp := Import{At: kw.pos}
	alias := p.peek()
	if alias.kind != tokIdent {
		p.errorf(alias, "expected an alias after import: import Sidebar \"./Sidebar.goi\"")
		p.sync()
		return imp, false
	}
	p.next()
	imp.Alias, imp.AliasAt = alias.text, alias.pos
	t := p.peek()
	if t.kind != tokString {
		p.errorf(t, "expected a quoted path after the import alias")
		p.sync()
		return imp, false
	}
	p.next()
	imp.Path = t.text
	if p.peek().kind == tokSemicolon {
		p.next()
	}
	return imp, true
}

// parseProps reads a fragment's input block, the typed values an instance
// passes and the fragment reads as `props.Name`:
//
//	props {
//		Title string
//		Rows  []ticket
//	}
func (p *parser) parseProps(f *File) {
	f.PropsAt = p.peek().pos
	p.next() // props
	if p.peek().kind != tokLBrace {
		p.errorf(p.peek(), "expected '{' after props")
		p.sync()
		return
	}
	p.next()
	p.parseFields(&f.Props, "prop")
}

// parseFields reads declarations until the block's '}', appending each to out.
// A declaration is `Name Type`, the way Go writes it.
func (p *parser) parseFields(out *[]Field, what string) {
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
			f, ok := p.parseField()
			if !ok {
				return
			}
			*out = append(*out, f)
		case tokAssign:
			p.errorf(t, "a declaration has no default; pass the value from the instance or Go")
			p.next()
		default:
			p.errorf(t, "expected a "+what+" name")
			if !p.recoverMember() {
				p.next()
			}
		}
	}
}

// parseProp reads one entry of a fragment's `props` block.
func (p *parser) parseProp() (Prop, bool) { return p.parseField() }

// parseField reads `Name Type`.
func (p *parser) parseField() (Field, bool) {
	name := p.peek()
	if name.kind != tokIdent {
		p.errorf(name, "expected a name")
		p.sync()
		return Field{}, false
	}
	p.next()
	f := Field{Name: name.text, NameAt: name.pos}
	typ, ok := p.parseType()
	if !ok {
		p.sync()
		return f, false
	}
	f.Type, f.At = typ, typ.At
	if p.peek().kind == tokSemicolon {
		p.next()
	}
	return f, true
}

// parseType reads a Go-like type: a primitive, `[]T`, `map[K]V` or `func(...)`.
func (p *parser) parseType() (Type, bool) {
	t := p.peek()
	switch {
	case t.kind == tokLBracket:
		open := p.next()
		if p.peek().kind != tokRBracket {
			p.errorf(p.peek(), "expected ']' after '[' in a slice type")
			return Type{}, false
		}
		p.next()
		elem, ok := p.parseType()
		if !ok {
			return Type{}, false
		}
		return Type{Kind: TypeSlice, Elem: &elem, At: open.pos}, true
	case t.kind == tokIdent && t.text == "map":
		at := p.next().pos
		if p.peek().kind != tokLBracket {
			p.errorf(p.peek(), "expected '[' after map")
			return Type{}, false
		}
		p.next()
		key := p.peek()
		if key.kind != tokIdent {
			p.errorf(key, "expected a map key type")
			return Type{}, false
		}
		p.next()
		if p.peek().kind != tokRBracket {
			p.errorf(p.peek(), "expected ']' after the map key type")
			return Type{}, false
		}
		p.next()
		elem, ok := p.parseType()
		if !ok {
			return Type{}, false
		}
		return Type{Kind: TypeMap, Name: key.text, Elem: &elem, At: at}, true
	case t.kind == tokIdent && t.text == "func":
		at := p.next().pos
		if p.peek().kind != tokLParen {
			p.errorf(p.peek(), "expected '(' after func")
			return Type{}, false
		}
		p.next()
		args, ok := p.parseFuncArgs()
		if !ok {
			return Type{}, false
		}
		return Type{Kind: TypeFunc, Args: args, At: at}, true
	case t.kind == tokIdent:
		p.next()
		return Type{Kind: TypeScalar, Name: t.text, At: t.pos}, true
	}
	p.errorf(t, "expected a type: a primitive, []T, map[K]V or func(...)")
	return Type{}, false
}

// parseFuncArgs reads `(id string, count uint)`, a handler's parameters.
func (p *parser) parseFuncArgs() ([]Field, bool) {
	var out []Field
	for {
		if p.peek().kind == tokRParen {
			p.next()
			return out, true
		}
		name := p.peek()
		if name.kind != tokIdent {
			p.errorf(name, "expected a parameter name")
			return nil, false
		}
		p.next()
		typ, ok := p.parseType()
		if !ok {
			return nil, false
		}
		out = append(out, Field{Name: name.text, NameAt: name.pos, Type: typ, At: typ.At})
		switch p.peek().kind {
		case tokComma, tokSemicolon:
			p.next()
		case tokRParen:
			p.next()
			return out, true
		default:
			p.errorf(p.peek(), "expected ',' or ')' after a parameter")
			return nil, false
		}
	}
}

// parseData reads either the variables block (`bridge { loading bool }`, also
// spelled `data { ... }`) or one row shape (`type ticket { title string }`).
func (p *parser) parseData(f *File) {
	kw := p.next() // "data", "bridge" or "type"
	name := p.peek()
	if name.kind == tokLBrace {
		if kw.text == "bridge" || kw.text == "data" {
			f.BridgeAt = kw.pos
		}
		p.next()
		p.parseFields(&f.Vars, "variable")
		return
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
	var fields []Field
	p.parseFields(&fields, "field")
	sh.Fields = fields
	if p.i > 0 && p.i-1 < len(p.toks) && p.toks[p.i-1].kind == tokRBrace {
		sh.Close = p.toks[p.i-1].pos
	}
	f.Shapes = append(f.Shapes, sh)
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

	ml := p.parseMembers()
	el.Members = ml.members
	el.Close = ml.close
	return el
}

// memberList is one parsed run of members plus how it ended.
type memberList struct {
	members []Member
	// close is the position of the '}' that closed the body.
	close Pos
	// closed is true when the '}' was consumed.
	closed bool
}

// parseMembers reads members until the body's '}'.
func (p *parser) parseMembers() memberList {
	// An element or an if/for body is a body of its own: a `case` in there is
	// not the end of an enclosing switch arm.
	saved := p.caseDepth
	p.caseDepth = 0
	ml := p.parseMemberList()
	p.caseDepth = saved
	return ml
}

// parseMemberList is parseMembers without the case-depth guard, for a switch
// arm, whose own `case` and `default` do end it.
func (p *parser) parseMemberList() memberList {
	var out memberList
	for {
		t := p.peek()
		switch t.kind {
		case tokEOF:
			p.errorf(t, "unexpected end of file: missing '}'")
			out.close = t.pos
			return out
		case tokRBrace:
			out.close = p.next().pos
			out.closed = true
			return out
		case tokSemicolon, tokComma:
			p.next() // stray separators are harmless
		case tokLBrace:
			if blk := p.parseOldBlock(); blk != nil {
				out.members = append(out.members, blk)
			}
		case tokIdent:
			if p.caseDepth > 0 && (t.text == "case" || t.text == "default") {
				out.close = t.pos
				return out // the switch takes over from here
			}
			if p.peek2().kind == tokColon {
				// `Ident :` is a property, `Ident {` is a child element.
				prop, ok := p.parseProperty()
				if ok {
					out.members = append(out.members, prop)
				}
				continue
			}
			switch t.text {
			case "if":
				if blk := p.parseIfBlock(); blk != nil {
					out.members = append(out.members, blk)
				}
				continue
			case "for":
				if blk := p.parseForBlock(); blk != nil {
					out.members = append(out.members, blk)
				}
				continue
			case "switch":
				if blk := p.parseSwitchBlock(); blk != nil {
					out.members = append(out.members, blk)
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
			p.errorf(t, "expected a property, a child element, if, for or switch")
			if !p.recoverMember() {
				p.next()
			}
		}
	}
}

// parseBlockBody reads one block body: `{ members }`. It returns the members and
// the position of the closing brace.
func (p *parser) parseBlockBody() ([]Member, Pos, bool) {
	open := p.peek()
	if open.kind != tokLBrace {
		p.errorf(open, "expected '{' to open the block body")
		return nil, open.pos, false
	}
	p.next()
	ml := p.parseMembers()
	return ml.members, ml.close, ml.closed
}

// parseIfBlock reads Go's if/else if/else:
//
//	if user.Adult {
//		Text { text: user.Name }
//	} else {
//		Text { text: "minor" }
//	}
func (p *parser) parseIfBlock() *Block {
	kw := p.next() // if
	blk := &Block{Kind: BlockIf, At: kw.pos}
	cond, ok := p.parseValue()
	if !ok {
		p.sync()
		return nil
	}
	blk.Branches = append(blk.Branches, Branch{Cond: cond, At: p.peek().pos})
	members, close, ok := p.parseBlockBody()
	if !ok {
		return nil
	}
	blk.Branches[0].Members = members
	blk.Close = close

	for p.peek().kind == tokIdent && p.peek().text == "else" {
		at := p.next().pos
		if p.peek().kind == tokIdent && p.peek().text == "if" {
			p.next()
			cond, ok := p.parseValue()
			if !ok {
				p.sync()
				return nil
			}
			arm := Branch{Cond: cond, At: at}
			members, close, ok := p.parseBlockBody()
			if !ok {
				return nil
			}
			arm.Members = members
			blk.Branches = append(blk.Branches, arm)
			blk.Close = close
			continue
		}
		arm := Branch{At: at}
		members, close, ok := p.parseBlockBody()
		if !ok {
			return nil
		}
		arm.Members = members
		blk.Branches = append(blk.Branches, arm)
		blk.Close = close
		break
	}
	return blk
}

// parseForBlock reads Go's range loop forms:
//
//	for i, row := range bridge.rows { ... }
//	for row := range bridge.rows { ... }   // the index, as in Go
//	for _, row := range bridge.rows { ... }
//	for i := range 5 { ... }
func (p *parser) parseForBlock() *Block {
	kw := p.next() // for
	blk := &Block{Kind: BlockFor, At: kw.pos}

	switch {
	case p.peek().kind == tokIdent && p.peek().text == "range":
		p.next()
	default:
		first := p.peek()
		if first.kind != tokIdent {
			p.errorf(first, "expected a loop variable, or range, after for")
			p.sync()
			return nil
		}
		p.next()
		if p.peek().kind == tokComma {
			p.next()
			second := p.peek()
			if second.kind != tokIdent {
				p.errorf(second, "expected a second loop variable after ','")
				p.sync()
				return nil
			}
			p.next()
			blk.KeyVar, blk.KeyAt = first.text, first.pos
			blk.ValVar, blk.ValAt = second.text, second.pos
		} else {
			blk.KeyVar, blk.KeyAt = first.text, first.pos
		}
		if p.peek().kind != tokDefine {
			p.errorf(p.peek(), "expected ':=' after the loop variable")
			p.sync()
			return nil
		}
		p.next()
		if p.peek().kind != tokIdent || p.peek().text != "range" {
			p.errorf(p.peek(), "expected range after ':='; a loop here is a range loop")
			p.sync()
			return nil
		}
		p.next()
	}

	rng := p.peek()
	blk.RangeAt = rng.pos
	v, ok := p.parseValue()
	if !ok {
		p.sync()
		return nil
	}
	blk.Range = v
	members, close, ok := p.parseBlockBody()
	if !ok {
		return nil
	}
	blk.Branches = []Branch{{Members: members}}
	blk.Close = close
	return blk
}

// parseSwitchBlock reads Go's switch, with comma-separated case values:
//
//	switch bridge.job {
//	case "swe", "eee":
//		Text { text: "engineer" }
//	default:
//		Text { text: "other" }
//	}
func (p *parser) parseSwitchBlock() *Block {
	kw := p.next() // switch
	blk := &Block{Kind: BlockSwitch, At: kw.pos, SubjectAt: p.peek().pos}
	subj, ok := p.parseValue()
	if !ok {
		p.sync()
		return nil
	}
	blk.Subject = subj
	if p.peek().kind != tokLBrace {
		p.errorf(p.peek(), "expected '{' to open the switch body")
		p.sync()
		return nil
	}
	p.next()

	for {
		t := p.peek()
		switch {
		case t.kind == tokRBrace:
			blk.Close = p.next().pos
			return blk
		case t.kind == tokEOF:
			p.errorf(t, "unexpected end of file: missing '}'")
			blk.Close = t.pos
			return blk
		case t.kind == tokIdent && t.text == "case":
			p.next()
			br := Branch{At: t.pos}
			for {
				v, ok := p.parseValue()
				if !ok {
					p.sync()
					return blk
				}
				br.Vals = append(br.Vals, v)
				if p.peek().kind != tokComma {
					break
				}
				p.next()
			}
			if p.peek().kind != tokColon {
				p.errorf(p.peek(), "expected ':' after a case value")
				p.sync()
				return blk
			}
			p.next()
			arm := p.parseArm()
			br.Members = arm.members
			blk.Branches = append(blk.Branches, br)
			if arm.closed {
				blk.Close = arm.close
				return blk
			}
		case t.kind == tokIdent && t.text == "default":
			p.next()
			if p.peek().kind != tokColon {
				p.errorf(p.peek(), "expected ':' after default")
				p.sync()
				return blk
			}
			p.next()
			arm := p.parseArm()
			blk.Branches = append(blk.Branches, Branch{At: t.pos, Members: arm.members})
			if arm.closed {
				blk.Close = arm.close
				return blk
			}
		default:
			p.errorf(t, "expected case, default or '}' in a switch body")
			p.sync()
			return blk
		}
	}
}

// parseArm reads one case arm's members, up to the next `case`, `default` or the
// switch's closing '}'.
func (p *parser) parseArm() memberList {
	p.caseDepth++
	ml := p.parseMemberList()
	p.caseDepth--
	return ml
}

// parseOldBlock reads the `{#if ...}` / `{#each ...}` spelling, which is gone,
// and reports it with the Go form that replaces it.
func (p *parser) parseOldBlock() *Block {
	open := p.next() // '{'
	if p.peek().kind != tokHash {
		p.errorf(open, "unexpected '{': a member is a property, a child element, an if, a for or a switch")
		p.sync()
		return nil
	}
	p.next()
	name := p.peek()
	hint := "if cond { ... } else { ... }"
	if name.kind == tokIdent && name.text == "each" {
		hint = "for i, row := range list { ... }"
	}
	p.errorf(open, "the {#if}/{#each} form is gone; write "+hint)
	p.sync()
	return nil
}

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
	ml := p.parseMembers()
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

	var v Value
	var ok bool
	switch name.text {
	case "shadow":
		v, ok = p.parseShadowValue()
	case "fontWeight":
		v, ok = p.parseFontWeight()
	case "decoration":
		v, ok = p.parseDecorationValue()
	case "cursor":
		v, ok = p.parseCursorValue()
	default:
		v, ok = p.parseValue()
	}
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

// sameLine reports whether a token at pos starts on the line the previous token
// ended on. A shadow is the one literal written as juxtaposed tokens, and
// newlines are not tokens, so without this it would swallow the next member.
func (p *parser) sameLine(pos Pos) bool {
	if p.i == 0 {
		return true
	}
	last := int(p.toks[p.i-1].end)
	if last > int(pos) {
		return true
	}
	return !bytes.ContainsRune(p.src[last:int(pos)], '\n')
}

// parseShadowValue reads a shadow list, or `none` for no shadow at all.
func (p *parser) parseShadowValue() (Value, bool) {
	if t := p.peek(); t.kind == tokIdent && t.text == "none" {
		p.next()
		return &Shadow{At: t.pos}, true
	}
	return p.parseShadow()
}

// Font weight keywords become the numbers layout and paint read. bolder and
// lighter stay relative, so they keep sentinel values that Text resolves
// against its type role; tree's weightBolder/weightLighter are the same pair.
// They are exported so the formatters can print the keyword back.
const (
	FontWeightBolder  = -1
	FontWeightLighter = -2
)

var fontWeightWords = map[string]float64{
	"normal":  400,
	"bold":    700,
	"bolder":  FontWeightBolder,
	"lighter": FontWeightLighter,
}

// parseFontWeight reads a CSS keyword or any expression that yields a number.
func (p *parser) parseFontWeight() (Value, bool) {
	if p.peek().kind == tokLBrace {
		return p.parseMapValue(p.parseFontWeight)
	}
	if t := p.peek(); t.kind == tokIdent {
		if n, ok := fontWeightWords[t.text]; ok {
			p.next()
			return &Number{At: t.pos, Val: n}, true
		}
	}
	return p.parseValue()
}

// decorationWords are the strokes `decoration` may combine. `line-through` is
// split by the lexer, so parseDecorationValue joins it back.
var decorationWords = map[string]bool{"underline": true, "overline": true}

// parseDecorationValue reads one or more keywords on the same line, so
// `decoration: underline overline` needs no quotes. Anything else — a quoted
// string, a reference, a breakpoint map — is an ordinary value.
func (p *parser) parseDecorationValue() (Value, bool) {
	if p.peek().kind == tokLBrace {
		return p.parseMapValue(p.parseDecorationValue)
	}
	at := p.peek().pos
	if !p.decorationStart() {
		return p.parseValue()
	}
	var words []string
	for {
		t := p.peek()
		if !p.sameLine(t.pos) {
			break
		}
		if t.kind == tokIdent && (decorationWords[t.text] || t.text == "none") {
			p.next()
			words = append(words, t.text)
			continue
		}
		if t.kind == tokIdent && t.text == "line" {
			m := p.peek2()
			if m.kind == tokMinus && p.sameLine(m.pos) {
				p.next() // line
				p.next() // -
				th := p.peek()
				if th.kind != tokIdent || th.text != "through" || !p.sameLine(th.pos) {
					p.errorf(th, "expected 'line-through'")
					return nil, false
				}
				p.next()
				words = append(words, "line-through")
				continue
			}
		}
		break
	}
	if len(words) == 0 {
		p.errorf(p.peek(), "expected a decoration keyword: none, underline, overline or line-through")
		return nil, false
	}
	// A word that is neither a decoration keyword nor the next property on a
	// single-line element is a typo: report it here instead of letting it look
	// like a new element.
	if t := p.peek(); p.sameLine(t.pos) && t.kind == tokIdent && p.peek2().kind != tokColon {
		p.errorf(t, "expected a decoration keyword: none, underline, overline or line-through")
		return nil, false
	}
	text := words[0]
	for _, w := range words[1:] {
		text += " " + w
	}
	return &String{At: at, Val: text}, true
}

// parseCursorValue reads one CSS cursor name. The lexer splits a hyphenated
// name (`not-allowed` becomes `not`, `-`, `allowed`), so the pieces are joined
// back into one identifier here. A breakpoint map or any other expression falls
// through to the ordinary value parser.
func (p *parser) parseCursorValue() (Value, bool) {
	if p.peek().kind == tokLBrace {
		return p.parseMapValue(p.parseCursorValue)
	}
	t := p.peek()
	if t.kind != tokIdent {
		return p.parseValue()
	}
	at := t.pos
	name := t.text
	p.next()
	for p.peek().kind == tokMinus && p.peek2().kind == tokIdent && p.sameLine(p.peek2().pos) {
		p.next() // -
		name += "-" + p.next().text
	}
	return &Ident{At: at, Val: name}, true
}

// decorationStart reports whether the next token begins a decoration keyword.
func (p *parser) decorationStart() bool {
	t := p.peek()
	if t.kind != tokIdent {
		return false
	}
	return decorationWords[t.text] || t.text == "none" || t.text == "line"
}

// parseShadow reads one or more CSS-style shadows, separated by commas:
//
//	x y [blur [spread]] [color] [inset]
func (p *parser) parseShadow() (Value, bool) {
	sh := &Shadow{At: p.peek().pos}
	for {
		layer, ok := p.parseShadowLayer()
		if !ok {
			return nil, false
		}
		sh.Layers = append(sh.Layers, layer)
		if p.peek().kind != tokComma {
			break
		}
		p.next()
	}
	return sh, true
}

// parseShadowLayer reads one shadow. The two offsets are required; blur, spread,
// colour and the `inset` keyword are optional, in that order, as in CSS.
func (p *parser) parseShadowLayer() (ShadowLayer, bool) {
	layer := ShadowLayer{At: p.peek().pos}
	x, ok := p.shadowNumber()
	if !ok {
		p.errorf(p.peek(), "a shadow starts with two offsets: `x y`, then an optional blur, spread, colour and inset")
		return layer, false
	}
	y, ok := p.shadowNumber()
	if !ok {
		p.errorf(p.peek(), "a shadow needs a second offset: `x y`")
		return layer, false
	}
	layer.X, layer.Y = x, y
	if n, ok := p.shadowNumber(); ok {
		layer.Blur = n
		if m, ok := p.shadowNumber(); ok {
			layer.Spread = m
		}
	}
	if t := p.peek(); p.sameLine(t.pos) {
		switch t.kind {
		case tokColor:
			p.next()
			layer.Color = &Color{At: t.pos, RGBA: t.rgba}
		case tokIdent:
			p.next()
			if t.text == "inset" {
				layer.Inset = true
			} else {
				layer.Color = &Ident{At: t.pos, Val: t.text}
			}
		}
	}
	if t := p.peek(); t.kind == tokIdent && t.text == "inset" && p.sameLine(t.pos) {
		p.next()
		layer.Inset = true
	}
	return layer, true
}

// shadowNumber reads one number of a shadow, including a negative offset. A
// `-` after a value is the subtraction operator everywhere else, so it is read
// here explicitly.
func (p *parser) shadowNumber() (float64, bool) {
	t := p.peek()
	if !p.sameLine(t.pos) {
		return 0, false
	}
	if t.kind == tokNumber {
		p.next()
		return t.num, true
	}
	if t.kind == tokMinus {
		n := p.peek2()
		if n.kind == tokNumber && p.sameLine(n.pos) {
			p.next()
			p.next()
			return -n.num, true
		}
	}
	return 0, false
}

func (p *parser) parseValue() (Value, bool) {
	t := p.peek()
	switch t.kind {
	case tokLBrace:
		return p.parseMap()
	case tokRBrace, tokEOF:
		p.errorf(t, "expected a value")
		return nil, false
	case tokError:
		// The lexer already reported this; stay quiet so the file does not
		// produce two diagnostics for one typo.
		return nil, false
	}
	// Every other value is an expression: a literal, reference or call, with
	// the operators and a conditional on top. Starting a bare identifier here
	// is what lets `i+1` work inside a call's arguments.
	return p.parseExpr()
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
	left, ok := p.parseUnary()
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
		right, ok := p.parseUnary()
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
		if p.peek().kind == tokLParen {
			// A bare call is a builtin: formatf, today the only one.
			return p.parseBuiltin(id.text, id.pos)
		}
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

// parseBuiltin reads `formatf("%d items", count)`, a call on a bare function
// name. Today formatf is the only builtin.
func (p *parser) parseBuiltin(name string, at Pos) (Value, bool) {
	p.next() // '('
	call := &Call{At: at, Name: name}
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
			p.errorf(p.peek(), "expected ',' or ')' after an argument")
			return nil, false
		}
	}
	p.next() // ')'
	return call, true
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
	return p.parseMapValue(p.parseValue)
}

// parseMapValue reads a breakpoint map whose entry values come from value. A
// property with its own literal form passes a parser that knows it, so
// `fontWeight: { md: bold }` keeps the keyword inside the map.
func (p *parser) parseMapValue(value func() (Value, bool)) (Value, bool) {
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
			v, ok := value()
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
