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

type parser struct {
	toks []token
	i    int
	diag Diagnostics
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
	// Tolerate leading junk but keep the diagnostics.
	root := p.parseElement()
	if root == nil {
		return nil
	}
	// Anything after the root element is an error, one per element.
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
	return &File{Root: root}
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
		default:
			p.errorf(t, "expected a property or child element")
			if !p.recoverMember() {
				p.next()
			}
		}
	}
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
	case tokNumber, tokLParen:
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

// parseExpr parses the additive level: a sum or difference of products.
func (p *parser) parseExpr() (Value, bool) {
	return p.parseAdd()
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

// parseTerm parses one number, percentage, reference, or a parenthesised
// expression. Expressions only use numeric values, so a bare identifier is
// always followed by `.property` here.
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
			p.errorf(p.peek(), "expected '.' after the id in an expression")
			return nil, false
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
		p.errorf(t, "expected a number or reference")
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
