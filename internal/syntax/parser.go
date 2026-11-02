package syntax

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
		if p, ok := e.Members[i].(*Property); ok {
			p.Src = src
			continue
		}
		if c, ok := e.Members[i].(*Element); ok {
			markSrc(c, src)
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
	// The preamble: `import ... as ...` and `param name: default` may appear in
	// any order before the root element.
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
		case t.text == "param":
			pr, ok := p.parseParam()
			if ok {
				f.Params = append(f.Params, pr)
			}
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

// parseParam reads `param name: default`.
func (p *parser) parseParam() (Param, bool) {
	kw := p.next() // param
	pr := Param{NameAt: kw.pos}
	name := p.peek()
	if name.kind != tokIdent {
		p.errorf(name, "expected a param name")
		p.sync()
		return pr, false
	}
	p.next()
	pr.Name, pr.NameAt = name.text, name.pos
	if p.peek().kind != tokColon {
		p.errorf(p.peek(), "expected ':' after the param name")
		p.sync()
		return pr, false
	}
	p.next()
	v, ok := p.parseValue()
	if !ok {
		p.sync()
		return pr, false
	}
	pr.Default = v
	if p.peek().kind == tokSemicolon {
		p.next()
	}
	return pr, true
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

	for {
		t := p.peek()
		switch t.kind {
		case tokEOF:
			p.errorf(t, "unexpected end of file: missing '}'")
			el.Close = t.pos
			return el
		case tokRBrace:
			el.Close = p.next().pos
			return el
		case tokSemicolon, tokComma:
			p.next() // stray separators are harmless
			continue
		case tokIdent:
			// `Ident :` is a property, `Ident {` is a child element.
			if p.peek2().kind == tokColon {
				prop, ok := p.parseProperty()
				if ok {
					el.Members = append(el.Members, prop)
				}
				continue
			}
			child := p.parseElement()
			if child != nil {
				el.Members = append(el.Members, child)
			}
		case tokDollar:
			if slot := p.parseSlot(); slot != nil {
				el.Members = append(el.Members, slot)
			}
		default:
			p.errorf(t, "expected a property or child element")
			if !p.recoverMember() {
				p.next()
			}
		}
	}
}

// parseSlot reads `$children`, the marker for where an instance's children go.
// It is the only name allowed after '$' as a member; `$param` is a value.
func (p *parser) parseSlot() *Element {
	dollar := p.next()
	name := p.peek()
	if name.kind != tokIdent {
		p.errorf(name, "expected a name after '$'")
		return nil
	}
	p.next()
	if name.text != "children" {
		p.errorf(dollar, "unknown slot $"+name.text+": only $children is a slot")
		return nil
	}
	p.slot = true
	return &Element{Name: "$children", NameAt: dollar.pos, Open: dollar.pos, Close: name.end}
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
	case tokString:
		p.next()
		return &String{At: t.pos, Val: t.text}, true
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
		at := p.next().pos
		name := p.peek()
		if name.kind != tokIdent {
			p.errorf(name, "expected a param name after '$'")
			return nil, false
		}
		p.next()
		return &ParamRef{At: at, Name: name.text}, true
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
	case tokBool:
		p.next()
		return &Bool{At: t.pos, Val: t.b}, true
	case tokColor:
		p.next()
		return &Color{At: t.pos, RGBA: t.rgba}, true
	case tokDollar:
		at := p.next().pos
		name := p.peek()
		if name.kind != tokIdent {
			p.errorf(name, "expected a param name after '$'")
			return nil, false
		}
		p.next()
		return &ParamRef{At: at, Name: name.text}, true
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
		return &Ref{At: id.pos, ID: id.text, Prop: prop.text}, true
	default:
		p.errorf(t, "expected a value or reference")
		return nil, false
	}
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
