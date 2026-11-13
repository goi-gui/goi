//go:build js

package platform

import "testing"

// TestCursorCSS pins the mapping a design's `cursor` reaches: every framework
// name has to become a CSS cursor, and an unknown one must not blank it.
func TestCursorCSS(t *testing.T) {
	cases := map[Cursor]string{
		CursorArrow:      "default",
		CursorPointer:    "pointer",
		CursorText:       "text",
		CursorMove:       "move",
		CursorNotAllowed: "not-allowed",
		CursorNone:       "none",
	}
	for c, want := range cases {
		if got := cursorCSS(c); got != want {
			t.Errorf("cursorCSS(%v) = %q, want %q", c, got, want)
		}
	}
}

// TestKeyFor pins the keyboard bridge: the named keys a design reacts to, the
// Ctrl chords it knows, and a printable key arriving as text.
func TestKeyFor(t *testing.T) {
	cases := []struct {
		name string
		ctrl bool
		key  Key
		text string
	}{
		{"Backspace", false, KeyBackspace, ""},
		{"ArrowLeft", false, KeyLeft, ""},
		{"Enter", false, KeyEnter, ""},
		{" ", false, KeySpace, ""},
		{"a", false, KeyNone, ""},
		{"A", false, KeyNone, ""},
		{"v", true, KeyV, ""},
		{"x", true, KeyX, ""},
		{"Escape", false, KeyEscape, ""},
		{"F5", false, KeyNone, ""},
	}
	for _, c := range cases {
		key, text, _ := keyFor(c.name, c.ctrl)
		if key != c.key || text != c.text {
			t.Errorf("keyFor(%q, ctrl=%v) = (%v, %q), want (%v, %q)",
				c.name, c.ctrl, key, text, c.key, c.text)
		}
	}
}
