package lsp

import (
	"bytes"
	"unicode/utf8"
)

// lineIndex maps byte offsets to LSP positions and back.
//
// LSP positions are line plus a column measured in UTF-16 code units, while the
// parser works in byte offsets. A design file is mostly ASCII, but a Turkish
// label or an emoji makes the two disagree, so every conversion goes through
// this type rather than counting bytes.
type lineIndex struct {
	starts []int // byte offset of each line start
	src    []byte
}

func newLineIndex(src []byte) lineIndex {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return lineIndex{starts: starts, src: src}
}

// line returns the 0-based line containing off.
func (l lineIndex) line(off int) int {
	lo, hi := 0, len(l.starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if l.starts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// position converts a byte offset into an LSP position.
func (l lineIndex) position(off int) position {
	if off < 0 {
		off = 0
	}
	if off > len(l.src) {
		off = len(l.src)
	}
	ln := l.line(off)
	start := l.starts[ln]
	return position{Line: ln, Character: utf16Len(l.src[start:off])}
}

// offset converts an LSP position into a byte offset, clamping past the end of
// a line or file instead of failing: a client may ask about a position that no
// longer exists after an edit.
func (l lineIndex) offset(p position) int {
	if p.Line < 0 {
		return 0
	}
	if p.Line >= len(l.starts) {
		return len(l.src)
	}
	start := l.starts[p.Line]
	end := len(l.src)
	if p.Line+1 < len(l.starts) {
		end = l.starts[p.Line+1]
	}
	// Drop the newline so a position at the end of a line stays on that line.
	line := bytes.TrimRight(l.src[start:end], "\n")
	line = bytes.TrimRight(line, "\r")
	want := p.Character
	if want <= 0 {
		return start
	}
	off := start
	units := 0
	for off < start+len(line) {
		r, size := utf8.DecodeRune(line[off-start:])
		n := 1
		if r > 0xFFFF {
			n = 2
		}
		if units+n > want {
			break
		}
		units += n
		off += size
	}
	return off
}

// utf16Len returns the UTF-16 code unit length of s.
func utf16Len(s []byte) int {
	n := 0
	for len(s) > 0 {
		r, size := utf8.DecodeRune(s)
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
		s = s[size:]
	}
	return n
}

// position is an LSP position: 0-based line and UTF-16 column.
type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// rng is an LSP range.
type rng struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

// at builds a range from two byte offsets.
func (l lineIndex) at(start, end int) rng {
	return rng{Start: l.position(start), End: l.position(end)}
}
