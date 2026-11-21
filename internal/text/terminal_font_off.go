//go:build !terminal && !all

package text

// TerminalFamily is the fallback monospace family when the terminal tag is off.
func TerminalFamily() string { return "Roboto Mono" }

func registerTerminalFont(l *Library) {}
