//go:build terminal || all

package text

import (
	"bytes"

	_ "embed"

	"github.com/go-text/typesetting/font"
)

// Meslo LG S NF Mono is the terminal face. It ships only when the terminal tag
// (or all) is on, because it is several megabytes.
//
//go:embed fonts/meslo-lg-nf-mono.ttf
var mesloNF []byte

const terminalFamily = "MesloLGS NF Mono"

// TerminalFamily is the monospace family a terminal draws with.
func TerminalFamily() string { return terminalFamily }

// registerTerminalFont adds the embedded terminal face to a new library.
func registerTerminalFont(l *Library) {
	if len(mesloNF) == 0 {
		return
	}
	if f, err := font.ParseTTF(bytes.NewReader(mesloNF)); err == nil {
		l.families[terminalFamily] = familyFace{face: f, tag: l.familyTag}
		l.familyTag += 2
	}
}
