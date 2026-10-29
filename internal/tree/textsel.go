package tree

// Text selection state for text fields. The caret is the moving end and
// SelAnchor the fixed end; the pair is enough to draw a range and to replace it.
// Positions are byte offsets into Text and are snapped to rune boundaries.

// Selection returns the selected byte range, or ok=false when nothing is
// selected. The range is normalized, so start <= end.
func (c *Component) Selection() (start, end int, ok bool) {
	if c.SelAnchor < 0 {
		return 0, 0, false
	}
	s := c.Text.Get()
	a, b := snapIndex(s, c.SelAnchor), snapIndex(s, c.Caret)
	if a > b {
		a, b = b, a
	}
	return a, b, a != b
}

// SelectAll selects the whole value.
func (c *Component) SelectAll() {
	n := len(c.Text.Get())
	if n == 0 {
		c.SelAnchor, c.Caret = -1, 0
		return
	}
	c.SelAnchor, c.Caret = 0, n
}

// ClearSelection drops the anchor without moving the caret.
func (c *Component) ClearSelection() { c.SelAnchor = -1 }

// MoveCaret moves the caret, extending the selection from the old caret when
// extend is true and collapsing it otherwise.
func (c *Component) MoveCaret(to int, extend bool) {
	to = snapIndex(c.Text.Get(), to)
	if extend {
		if c.SelAnchor < 0 {
			c.SelAnchor = c.Caret
		}
	} else {
		c.SelAnchor = -1
	}
	c.Caret = to
}

// snapIndex clamps i to the string and moves it back to a rune boundary, so a
// caller can never select half a UTF-8 sequence.
func snapIndex(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i >= len(s) {
		return len(s)
	}
	for i > 0 && s[i]&0xC0 == 0x80 {
		i--
	}
	return i
}

// SetPreedit replaces the IME composition text and its caret. The caret is a
// byte index snapped to a rune boundary; a negative value means the end.
func (c *Component) SetPreedit(text string, caret int) {
	if caret < 0 || caret > len(text) {
		caret = len(text)
	}
	c.Preedit = text
	c.PreeditCaret = snapIndex(text, caret)
}

// ClearPreedit drops the composition without touching the value.
func (c *Component) ClearPreedit() {
	c.Preedit = ""
	c.PreeditCaret = 0
}
