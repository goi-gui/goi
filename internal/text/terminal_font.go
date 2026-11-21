//go:build terminal || all

package text

import (
	"bytes"

	_ "embed"

	"github.com/go-text/typesetting/font"
)

// Maple Mono NF CN is the terminal face: monospace Latin, Nerd Font icons and
// CJK in one file. It ships only when the terminal tag (or all) is on, because
// it is about 20 MB.
//
//go:embed fonts/maple-mono-nf-cn.ttf
var terminalTTF []byte

const terminalFamily = "Maple Mono NF CN"

// TerminalFamily is the monospace family a terminal draws with.
func TerminalFamily() string { return terminalFamily }

// registerTerminalFont adds the embedded terminal face to a new library.
func registerTerminalFont(l *Library) {
	if len(terminalTTF) == 0 {
		return
	}
	if f, err := font.ParseTTF(bytes.NewReader(terminalTTF)); err == nil {
		l.families[terminalFamily] = familyFace{face: f, tag: l.familyTag}
		l.familyTag += 2
	}
}
