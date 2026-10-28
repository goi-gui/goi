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
		case tokSemicolon:
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
	p.next()
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
		return &String{At: t.pos, Val: t.text}, true
	case tokNumber:
		return &Number{At: t.pos, Val: t.num}, true
	case tokBool:
		return &Bool{At: t.pos, Val: t.b}, true
	case tokColor:
		return &Color{At: t.pos, RGBA: t.rgba}, true
	case tokIdent:
		return &Ident{At: t.pos, Val: t.text}, true
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
