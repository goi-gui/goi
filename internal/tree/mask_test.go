package tree

import (
	"strings"
	"testing"
)

// TestInputTypeFilters: the type drops anything its class does not allow, and a
// value that is not one of the three names is the set itself.
func TestInputTypeFilters(t *testing.T) {
	cases := []struct {
		kind, in, want string
	}{
		{"", "a1-_", "a1-_"},
		{"text", "a1-_", "a1-_"},
		{"numeric", "a1b2c", "12"},
		{"alpha", "a1b2c", "abc"},
		{"alphanumeric", "a1-_b2", "a1b2"},
		{"abc", "adbc", "abc"},
		{"0123456789.", "a1.2,b3", "1.23"},
		// The set is case-sensitive: a lowercase hex field drops #FF.
		{"0123456789abcdef", "#FF00", "00"},
		{"0123456789abcdefABCDEF#", "#FF00", "#FF00"},
	}
	for _, c := range cases {
		got, caret := InputInsert(c.kind, "", "", 0, 0, c.in)
		if got != c.want {
			t.Errorf("type %q of %q = %q, want %q", c.kind, c.in, got, c.want)
		}
		if caret != len(c.want) {
			t.Errorf("type %q caret = %d, want %d", c.kind, caret, len(c.want))
		}
	}
}

// TestInputMaskTypesAndSteps walks a phone number in one digit at a time: the
// mask inserts its literals, and the caret lands after the last digit.
func TestInputMaskTypesAndSteps(t *testing.T) {
	const mask = "(###) ###-####"
	text, caret := "", 0
	for _, r := range "5551234567" {
		text, caret = InputInsert("", mask, text, caret, caret, string(r))
	}
	if text != "(555) 123-4567" {
		t.Fatalf("masked text = %q", text)
	}
	if caret != len(text) {
		t.Fatalf("caret = %d, want %d", caret, len(text))
	}

	// A literal is inserted by the mask, so typing one adds nothing.
	text, caret = InputInsert("", mask, text, 0, 0, "(")
	if text != "(555) 123-4567" || caret != 0 {
		t.Fatalf("typing a literal changed the value: %q caret %d", text, caret)
	}
}

// TestInputMaskBackspaceAndDelete: both skip the literals and work on the
// entered characters.
func TestInputMaskBackspaceAndDelete(t *testing.T) {
	const mask = "(###) ###-####"
	text := "(555) 123-4567"

	got, caret := InputDelete("", mask, text, len(text), true)
	if got != "(555) 123-456" || caret != len(got) {
		t.Fatalf("backspace = %q caret %d", got, caret)
	}
	// Backspace once more, over the "-" literal: the 5 before it goes.
	got, caret = InputDelete("", mask, got, caret, true)
	if got != "(555) 123-45" || caret != len(got) {
		t.Fatalf("backspace over a literal = %q caret %d", got, caret)
	}
	// Delete at the start drops the first digit and shifts the rest left.
	got, caret = InputDelete("", mask, text, 0, false)
	if got != "(551) 234-567" || caret != 0 {
		t.Fatalf("delete = %q caret %d", got, caret)
	}
}

// TestInputMaskAppliesType: a character must pass the type and fit the slot.
func TestInputMaskAppliesType(t *testing.T) {
	got, caret := InputInsert("numeric", "##-##", "", 0, 0, "12ab34")
	if got != "12-34" || caret != len(got) {
		t.Fatalf("numeric mask = %q caret %d", got, caret)
	}
}

// TestInputMaskRemoveSelection drops the entered characters a selection covers
// and keeps the literals.
func TestInputMaskRemoveSelection(t *testing.T) {
	const mask = "(###) ###-####"
	// Select the three digits after the opening parenthesis.
	got, _ := InputRemove("", mask, "(555) 123-4567", 1, 4)
	if got != "(123) 456-7" {
		t.Fatalf("removed selection = %q", got)
	}
}

// TestInputMaskInsertInMiddle shifts the rest right and fills the caret slot.
func TestInputMaskInsertInMiddle(t *testing.T) {
	const mask = "(###) ###-####"
	got, caret := InputInsert("", mask, "(555) 123-4567", 3, 3, "9")
	if got != "(559) 512-3456" {
		t.Fatalf("middle insert = %q", got)
	}
	if caret != 4 {
		t.Fatalf("caret = %d, want 4 (after the inserted 9)", caret)
	}
}

// TestInputMaskEmptyKeepsNoLiterals: a partial value has no dangling
// punctuation, and an empty mask leaves the text alone.
func TestInputMaskEmptyKeepsNoLiterals(t *testing.T) {
	if got, caret := InputInsert("", "(###) ###", "", 0, 0, "5"); got != "(5" || caret != 2 {
		t.Fatalf("first digit = %q caret %d", got, caret)
	}
	if got, caret := InputInsert("", "", "abc", 1, 1, "x"); got != "axbc" || caret != 2 {
		t.Fatalf("no mask = %q caret %d", got, caret)
	}
}

// TestInputTypeRejectsSingleCharacter: one rejected keystroke leaves the field
// and its selection untouched.
func TestInputTypeRejectsSingleCharacter(t *testing.T) {
	text := "12"
	got, caret := InputInsert("numeric", "", text, 2, 2, "a")
	if got != text || caret != 2 {
		t.Fatalf("rejected key = %q caret %d", got, caret)
	}
	// A paste keeps whatever does fit instead of dropping all of it.
	got, _ = InputInsert("numeric", "", text, 2, 2, "a3b4")
	if got != "1234" {
		t.Fatalf("paste = %q", got)
	}
}

// TestInputMaskLiteralsOnlyMask: with no slots the mask never accepts input.
func TestInputMaskLiteralsOnlyMask(t *testing.T) {
	if got, _ := InputInsert("", "abc", "", 0, 0, "x"); got != "" {
		t.Fatalf("literal-only mask accepted %q", got)
	}
}

// TestInputMaskTruncatesAtTheLastSlot: a full field drops the overflow.
func TestInputMaskTruncatesAtTheLastSlot(t *testing.T) {
	got, _ := InputInsert("", "##", "", 0, 0, "12345")
	if got != "12" {
		t.Fatalf("overflow = %q", got)
	}
	if strings.Contains(got, "#") {
		t.Fatalf("mask leaked into the value: %q", got)
	}
}

// TestInputMaskEscapes: a backslash makes a slot character literal, and a
// double backslash is a literal backslash.
func TestInputMaskEscapes(t *testing.T) {
	// \# is a hash to show, then # is the digit slot.
	got, caret := InputInsert("", `\##`, "", 0, 0, "5")
	if got != "#5" || caret != 2 {
		t.Fatalf("escaped hash = %q caret %d, want #5", got, caret)
	}
	// A digit does not fit the literal, so nothing is entered before the slot.
	got, _ = InputInsert("", `\#`, "", 0, 0, "5")
	if got != "" {
		t.Fatalf("escaped hash took input: %q", got)
	}
	// \\ is one literal backslash, then a digit slot.
	got, _ = InputInsert("", `\\#`, "", 0, 0, "5")
	if got != `\5` {
		t.Fatalf("escaped backslash = %q", got)
	}
}

// TestInputMaskEscapeSkipsLiteralsOnDelete still treats the escaped rune as a
// literal: backspace removes the entered digit, never the shown punctuation.
func TestInputMaskEscapeSkipsLiteralsOnDelete(t *testing.T) {
	got, caret := InputDelete("", `\##`, "#5", 2, true)
	if got != "" || caret != 0 {
		t.Fatalf("backspace = %q caret %d, want the empty value", got, caret)
	}
}
