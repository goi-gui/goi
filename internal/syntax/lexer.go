package syntax

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokString
	tokNumber
	tokColor
	tokLBrace
	tokRBrace
	tokColon
	tokComma
	tokSemicolon
	tokBool
	tokError
)

type token struct {
	kind tokenKind
	pos  Pos
	end  Pos
	text string  // identifier text, or the decoded string value
	num  float64 // for tokNumber
	b    bool    // for tokBool
	rgba uint32  // for tokColor
	err  string  // for tokError
}

type lexer struct {
	src  []byte
	off  int
	toks []token
	diag Diagnostics
}

func newLexer(src []byte) *lexer { return &lexer{src: src} }

func (l *lexer) run() ([]token, Diagnostics) {
	for {
		l.skipSpaceAndComments()
		if l.off >= len(l.src) {
			l.emit(token{kind: tokEOF, pos: Pos(l.off), end: Pos(l.off)})
			return l.toks, l.diag
		}
		start := l.off
		c := l.src[l.off]
		switch {
		case c == '{':
			l.off++
			l.emit(token{kind: tokLBrace, pos: Pos(start), end: Pos(l.off)})
		case c == '}':
			l.off++
			l.emit(token{kind: tokRBrace, pos: Pos(start), end: Pos(l.off)})
		case c == ':':
			l.off++
			l.emit(token{kind: tokColon, pos: Pos(start), end: Pos(l.off)})
		case c == ',':
			l.off++
			l.emit(token{kind: tokComma, pos: Pos(start), end: Pos(l.off)})
		case c == ';':
			l.off++
			l.emit(token{kind: tokSemicolon, pos: Pos(start), end: Pos(l.off)})
		case c == '"':
			l.lexString(start)
		case c == '#':
			l.lexColor(start)
		case c == '-' || (c >= '0' && c <= '9'):
			l.lexNumber(start)
		case c == '_' || isLetter(c):
			l.lexIdent(start)
		default:
			r, size := utf8.DecodeRune(l.src[l.off:])
			l.off += size
			l.fail(start, l.off, "unexpected character "+quoteRune(r))
			l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off)})
		}
	}
}

func (l *lexer) emit(t token) { l.toks = append(l.toks, t) }

func (l *lexer) fail(start, end int, msg string) {
	l.diag = append(l.diag, Diagnostic{Pos: Pos(start), End: Pos(end), Message: msg})
}

func (l *lexer) skipSpaceAndComments() {
	for l.off < len(l.src) {
		c := l.src[l.off]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			l.off++
		case c == '/' && l.off+1 < len(l.src) && l.src[l.off+1] == '/':
			for l.off < len(l.src) && l.src[l.off] != '\n' {
				l.off++
			}
		case c == '/' && l.off+1 < len(l.src) && l.src[l.off+1] == '*':
			start := l.off
			l.off += 2
			for l.off+1 < len(l.src) && !(l.src[l.off] == '*' && l.src[l.off+1] == '/') {
				l.off++
			}
			if l.off+1 >= len(l.src) {
				l.off = len(l.src)
				l.fail(start, l.off, "unterminated block comment")
				return
			}
			l.off += 2
		default:
			return
		}
	}
}

func (l *lexer) lexIdent(start int) {
	for l.off < len(l.src) && (l.src[l.off] == '_' || isLetter(l.src[l.off]) || isDigit(l.src[l.off])) {
		l.off++
	}
	text := string(l.src[start:l.off])
	if text == "true" || text == "false" {
		l.emit(token{kind: tokBool, pos: Pos(start), end: Pos(l.off), b: text == "true", text: text})
		return
	}
	l.emit(token{kind: tokIdent, pos: Pos(start), end: Pos(l.off), text: text})
}

func (l *lexer) lexNumber(start int) {
	if l.src[l.off] == '-' {
		l.off++
		if l.off >= len(l.src) || !isDigit(l.src[l.off]) {
			l.fail(start, l.off, "expected a digit after '-'")
			l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off)})
			return
		}
	}
	for l.off < len(l.src) && isDigit(l.src[l.off]) {
		l.off++
	}
	if l.off < len(l.src) && l.src[l.off] == '.' {
		l.off++
		if l.off >= len(l.src) || !isDigit(l.src[l.off]) {
			l.fail(start, l.off, "expected a digit after '.'")
			l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off)})
			return
		}
		for l.off < len(l.src) && isDigit(l.src[l.off]) {
			l.off++
		}
	}
	text := string(l.src[start:l.off])
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		l.fail(start, l.off, "invalid number "+text)
		l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off)})
		return
	}
	l.emit(token{kind: tokNumber, pos: Pos(start), end: Pos(l.off), num: f, text: text})
}

func (l *lexer) lexString(start int) {
	l.off++ // opening quote
	var b strings.Builder
	for {
		if l.off >= len(l.src) {
			l.fail(start, l.off, "unterminated string")
			l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off)})
			return
		}
		c := l.src[l.off]
		if c == '"' {
			l.off++
			l.emit(token{kind: tokString, pos: Pos(start), end: Pos(l.off), text: b.String()})
			return
		}
		if c != '\\' {
			if c == '\n' {
				l.fail(start, l.off, "unterminated string")
				l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off)})
				return
			}
			r, size := utf8.DecodeRune(l.src[l.off:])
			b.WriteRune(r)
			l.off += size
			continue
		}
		// Escape sequence.
		if l.off+1 >= len(l.src) {
			l.fail(start, l.off+1, "unterminated escape sequence")
			l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off + 1)})
			l.off = len(l.src)
			return
		}
		esc := l.src[l.off+1]
		l.off += 2
		switch esc {
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '/':
			b.WriteByte('/')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'u':
			r, ok := l.hex4()
			if !ok {
				l.fail(start, l.off, "invalid \\u escape: expected 4 hex digits")
				l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off)})
				return
			}
			b.WriteRune(r)
		default:
			l.fail(start, l.off, "invalid escape sequence \\"+string(esc))
			l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off)})
			return
		}
	}
}

// hex4 reads 4 hex digits after \u, joining a surrogate pair when present.
func (l *lexer) hex4() (rune, bool) {
	if l.off+4 > len(l.src) {
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		d := hexVal(l.src[l.off+i])
		if d < 0 {
			return 0, false
		}
		r = r*16 + rune(d)
	}
	l.off += 4
	if r >= 0xD800 && r <= 0xDBFF && l.off+6 <= len(l.src) && l.src[l.off] == '\\' && l.src[l.off+1] == 'u' {
		var lo rune
		ok := true
		for i := 0; i < 4; i++ {
			d := hexVal(l.src[l.off+2+i])
			if d < 0 {
				ok = false
				break
			}
			lo = lo*16 + rune(d)
		}
		if ok && lo >= 0xDC00 && lo <= 0xDFFF {
			l.off += 6
			return 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00), true
		}
	}
	if r >= 0xD800 && r <= 0xDFFF {
		return 0xFFFD, true // lone surrogate: replacement char, not a parse error
	}
	return r, true
}

func (l *lexer) lexColor(start int) {
	l.off++ // '#'
	hexStart := l.off
	for l.off < len(l.src) && hexVal(l.src[l.off]) >= 0 {
		l.off++
	}
	n := l.off - hexStart
	var rgba uint32
	switch n {
	case 3:
		var r, g, b uint32
		for i, p := range []*uint32{&r, &g, &b} {
			d := uint32(hexVal(l.src[hexStart+i]))
			*p = d*16 + d
		}
		rgba = r<<24 | g<<16 | b<<8 | 0xFF
	case 6, 8:
		rgba = 0
		for i := 0; i < n; i++ {
			rgba = rgba<<4 | uint32(hexVal(l.src[hexStart+i]))
		}
		if n == 6 {
			rgba = rgba<<8 | 0xFF
		}
	default:
		l.fail(start, l.off, "color must be #rgb, #rrggbb or #rrggbbaa")
		l.emit(token{kind: tokError, pos: Pos(start), end: Pos(l.off)})
		return
	}
	l.emit(token{kind: tokColor, pos: Pos(start), end: Pos(l.off), rgba: rgba})
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func quoteRune(r rune) string {
	if unicode.IsPrint(r) {
		return "'" + string(r) + "'"
	}
	return "U+" + strings.ToUpper(hexString(uint32(r)))
}

func hexString(v uint32) string {
	const digits = "0123456789abcdef"
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v&0xF]
		v >>= 4
	}
	return string(buf[i:])
}
