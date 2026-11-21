package tree

import (
	"io"
	"sync"
	"unicode/utf8"
)

// Terminal is a character-cell terminal with ANSI/VT100 colours, cursor
// movement, scrolling and an alternate screen. The app attaches a tty with
// SetReader and SetWriter; the design gives the box its size.
type Terminal struct {
	Component
	FontSize   Prop[float64]
	Scrollback Prop[float64]
	Foreground Prop[uint32]
	// Resize fires when the grid measured from the box changes, which is what a
	// pty needs for its window size.
	Resize Event[TerminalSize]

	mu      sync.Mutex
	state   terminalState
	normal  terminalState
	alt     bool
	parser  terminalParser
	pending []byte
	writer  io.Writer
	reader  io.Reader
	scroll  int
	fg, bg  uint32
	attr    termAttr
}

// TerminalSize is the character grid a terminal measured.
type TerminalSize struct {
	Cols, Rows int
}

// Kind implements Node.
func (*Terminal) Kind() string { return "Terminal" }

func (t *Terminal) namedProps() []NamedProp {
	b := &t.NodeBase
	return append(t.propsExcept("Terminal", "fontSize", "scrollback", "foreground"),
		NumberProp(b, "fontSize", DirtyLayout, &t.FontSize),
		NumberProp(b, "scrollback", DirtyPaint, &t.Scrollback),
		ColorProp(b, "foreground", DirtyPaint, &t.Foreground),
	)
}

// SetReader starts a reader goroutine that feeds the terminal. The goroutine
// ends at EOF; replacing it does not close the previous reader.
func (t *Terminal) SetReader(r io.Reader) {
	t.mu.Lock()
	t.reader = r
	t.mu.Unlock()
	if r == nil {
		return
	}
	tr := t.tree
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				data := append([]byte(nil), buf[:n]...)
				if tr != nil {
					tr.Post(func() { t.Feed(data) })
				} else {
					t.Feed(data)
				}
			}
			if err != nil {
				return
			}
		}
	}()
}

// SetWriter is the writer keyboard input goes to, usually the other end of a
// pty. A nil writer drops input.
func (t *Terminal) SetWriter(w io.Writer) {
	t.mu.Lock()
	t.writer = w
	t.mu.Unlock()
}

// Feed parses bytes the app read from the tty. It is safe to call for a whole
// chunk; an incomplete UTF-8 rune waits for the next call.
func (t *Terminal) Feed(data []byte) {
	if len(data) == 0 {
		return
	}
	t.mu.Lock()
	if t.state.rows == 0 {
		t.state = newTerminalState(80, 24)
		t.state.maxBack = t.maxBack()
	}
	t.pending = append(t.pending, data...)
	t.consume()
	t.mu.Unlock()
	t.MarkDirty(DirtyPaint)
}

// FeedString is Feed with a string.
func (t *Terminal) FeedString(s string) { t.Feed([]byte(s)) }

// WriteInput sends text to the writer, as if the user typed it.
func (t *Terminal) WriteInput(s string) {
	t.mu.Lock()
	w := t.writer
	t.mu.Unlock()
	if w != nil && s != "" {
		_, _ = io.WriteString(w, s)
	}
}

// Grid returns the current character grid.
func (t *Terminal) Grid() TerminalSize {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TerminalSize{Cols: t.state.cols, Rows: t.state.rows}
}

// OnResize subscribes to grid changes. The callback runs on the UI goroutine.
func (t *Terminal) OnResize(fn func(TerminalSize)) (off func()) { return t.Resize.On(fn) }

// ScrollBy moves the scrollback by n lines; positive moves into history. It
// reports whether the view changed.
func (t *Terminal) ScrollBy(n int) bool {
	if n == 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	old := t.scroll
	t.scroll += n
	if t.scroll < 0 {
		t.scroll = 0
	}
	if max := len(t.state.scroll); t.scroll > max {
		t.scroll = max
	}
	if t.scroll != old {
		t.MarkDirty(DirtyPaint)
		return true
	}
	return false
}

// terminalState is one screen. A terminal keeps a main and an alternate one so
// a full-screen program can restore the shell when it exits.
type terminalState struct {
	lines    []termLine
	scroll   []termLine
	rows     int
	cols     int
	row      int
	col      int
	savedR   int
	savedC   int
	top      int
	bottom   int
	wrap     bool
	maxBack  int
	altState bool
	cursor   bool
	autowrap bool
}

type termLine []termCell

type termCell struct {
	r    rune
	fg   uint32
	bg   uint32
	attr termAttr
}

type termAttr uint8

const (
	termBold termAttr = 1 << iota
	termFaint
	termItalic
	termUnderline
	termReverse
	termStrike
	termHidden
)

func newTerminalState(cols, rows int) terminalState {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	s := terminalState{rows: rows, cols: cols, bottom: rows - 1, cursor: true, autowrap: true}
	s.lines = make([]termLine, rows)
	for i := range s.lines {
		s.lines[i] = blankTermLine(cols)
	}
	return s
}

func blankTermLine(cols int) termLine {
	line := make(termLine, cols)
	for i := range line {
		line[i].r = ' '
	}
	return line
}

func (s *terminalState) resize(cols, rows, maxBack int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	old := s.lines
	s.rows, s.cols = rows, cols
	s.lines = make([]termLine, rows)
	for i := range s.lines {
		s.lines[i] = blankTermLine(cols)
		if i < len(old) {
			copy(s.lines[i], old[i])
		}
	}
	if s.row >= rows {
		s.row = rows - 1
	}
	if s.col >= cols {
		s.col = cols - 1
	}
	s.top, s.bottom = 0, rows-1
	s.maxBack = maxBack
	s.wrap = false
}

func (s *terminalState) cell(row, col int) *termCell {
	if row < 0 {
		row = 0
	}
	if row >= s.rows {
		row = s.rows - 1
	}
	if col < 0 {
		col = 0
	}
	if col >= s.cols {
		col = s.cols - 1
	}
	return &s.lines[row][col]
}

func (s *terminalState) clearLine(row int) {
	for i := range s.lines[row] {
		s.lines[row][i] = termCell{r: ' '}
	}
}

func (s *terminalState) eraseRows(from, to int) {
	for r := from; r <= to && r < s.rows; r++ {
		if r >= 0 {
			s.clearLine(r)
		}
	}
}

func (s *terminalState) scrollUp(n int) {
	if n <= 0 {
		return
	}
	if n > s.bottom-s.top+1 {
		n = s.bottom - s.top + 1
	}
	if !s.noScrollback() {
		for i := 0; i < n; i++ {
			s.scroll = append(s.scroll, append(termLine(nil), s.lines[s.top]...))
		}
		if s.maxBack > 0 && len(s.scroll) > s.maxBack {
			s.scroll = s.scroll[len(s.scroll)-s.maxBack:]
		}
	}
	copy(s.lines[s.top:s.bottom+1], s.lines[s.top+n:s.bottom+1])
	for i := 0; i < n; i++ {
		s.lines[s.bottom-i] = blankTermLine(s.cols)
	}
}

func (s *terminalState) noScrollback() bool { return s.top != 0 || s.altState }

// altState is set on the alternate screen, whose contents never enter history.
// It is a field on the state so scrollUp can read it without the terminal.
func (s *terminalState) setAlt(v bool) { s.altState = v }

func (s *terminalState) scrollDown(n int) {
	if n <= 0 {
		return
	}
	if n > s.bottom-s.top+1 {
		n = s.bottom - s.top + 1
	}
	copy(s.lines[s.top+n:s.bottom+1], s.lines[s.top:s.bottom+1-n])
	for i := 0; i < n; i++ {
		s.lines[s.top+i] = blankTermLine(s.cols)
	}
}

// altState marks a screen as the alternate buffer.
// (Kept beside the state so the parser does not need a second argument.)

// TerminalCell is one painted character cell. Zero FG/BG means the terminal's
// default colour.
type TerminalCell struct {
	R                 rune
	FG, BG            uint32
	Bold, Faint       bool
	Italic, Underline bool
	Reverse, Strike   bool
	Hidden            bool
}

// TerminalView is the visible grid plus the cursor, with scrollback applied.
type TerminalView struct {
	Lines      [][]TerminalCell
	CursorRow  int
	CursorCol  int
	CursorOn   bool
	Cols, Rows int
}

// View returns the rows a painter should draw.
func (t *Terminal) View(rows int) TerminalView {
	t.mu.Lock()
	defer t.mu.Unlock()
	if rows < 1 {
		rows = t.state.rows
	}
	v := TerminalView{Rows: rows, Cols: t.state.cols, CursorRow: -1, CursorCol: -1, CursorOn: t.state.cursor}
	total := len(t.state.scroll) + len(t.state.lines)
	end := total - t.scroll
	if t.scroll == 0 {
		end = total
		v.CursorRow = t.state.row
		v.CursorCol = t.state.col
	}
	start := end - rows
	for r := 0; r < rows; r++ {
		idx := start + r
		line := make([]TerminalCell, t.state.cols)
		for c := range line {
			line[c].R = ' '
		}
		if idx >= 0 && idx < total {
			var src termLine
			if idx < len(t.state.scroll) {
				src = t.state.scroll[idx]
			} else if idx-len(t.state.scroll) < len(t.state.lines) {
				src = t.state.lines[idx-len(t.state.scroll)]
			}
			for c := range line {
				if c >= len(src) {
					break
				}
				cell := src[c]
				line[c] = TerminalCell{
					R: cell.r, FG: cell.fg, BG: cell.bg,
					Bold: cell.attr&termBold != 0, Faint: cell.attr&termFaint != 0,
					Italic: cell.attr&termItalic != 0, Underline: cell.attr&termUnderline != 0,
					Reverse: cell.attr&termReverse != 0, Strike: cell.attr&termStrike != 0,
					Hidden: cell.attr&termHidden != 0,
				}
			}
		}
		v.Lines = append(v.Lines, line)
	}
	return v
}

// ScrollLines reports how many lines of history the terminal holds.
func (t *Terminal) ScrollLines() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.state.scroll)
}

func (t *Terminal) maxBack() int {
	n := int(t.Scrollback.Get())
	if n < 0 {
		return 0
	}
	return n
}

// Fit sets the character grid measured from the painted box. A design can pin
// it with cols and rows; otherwise the painter measures the available space.
func (t *Terminal) Fit(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	t.mu.Lock()
	if cols == t.state.cols && rows == t.state.rows {
		t.mu.Unlock()
		return
	}
	t.state.resize(cols, rows, t.maxBack())
	if t.alt {
		t.normal.resize(cols, rows, t.maxBack())
	}
	if t.scroll > len(t.state.scroll) {
		t.scroll = len(t.state.scroll)
	}
	t.mu.Unlock()
	t.Resize.Emit(TerminalSize{Cols: cols, Rows: rows})
	t.MarkDirty(DirtyPaint)
}

// terminalParser is the byte state machine for escape sequences.
type terminalParser struct {
	state   termParseState
	params  []int
	cur     int
	hasCur  bool
	private bool
}

type termParseState uint8

const (
	termGround termParseState = iota
	termEscape
	termCSI
	termOSC
	termOSCEsc
	termCharset
)

func (p *terminalParser) reset() {
	p.state = termGround
	p.params = p.params[:0]
	p.cur = 0
	p.hasCur = false
	p.private = false
}

func (p *terminalParser) pushParam() {
	p.params = append(p.params, p.cur)
	p.cur = 0
	p.hasCur = false
}

func (t *Terminal) consume() {
	for len(t.pending) > 0 {
		if t.parser.state == termGround {
			r, size := utf8.DecodeRune(t.pending)
			if r == utf8.RuneError && size == 1 && !utf8.FullRune(t.pending) {
				return
			}
			t.pending = t.pending[size:]
			t.handleRune(r)
			continue
		}
		b := t.pending[0]
		t.pending = t.pending[1:]
		t.handleByte(b)
	}
}

func (t *Terminal) handleRune(r rune) {
	switch r {
	case 0x1b:
		t.parser.state = termEscape
		return
	}
	if t.parser.state != termGround {
		return
	}
	s := &t.state
	switch r {
	case '\n':
		s.lineFeed()
	case '\r':
		s.col = 0
		s.wrap = false
	case '\b':
		if s.col > 0 {
			s.col--
		}
		s.wrap = false
	case '\t':
		n := (s.col/8 + 1) * 8
		if n >= s.cols {
			n = s.cols - 1
		}
		s.col = n
		s.wrap = false
	case 0x07:
		// bell: no sound in a GUI, and nothing to draw
	default:
		if r >= 0x20 {
			t.put(r)
		}
	}
}

func (t *Terminal) handleByte(b byte) {
	p := &t.parser
	switch p.state {
	case termEscape:
		switch b {
		case '[':
			p.state = termCSI
			p.params = p.params[:0]
			p.cur, p.hasCur, p.private = 0, false, false
		case ']':
			p.state = termOSC
		case 'P', 'X', '^', '_':
			p.state = termOSC
		case '7':
			t.state.savedR, t.state.savedC = t.state.row, t.state.col
			p.state = termGround
		case '8':
			t.state.row, t.state.col = t.state.savedR, t.state.savedC
			t.state.wrap = false
			p.state = termGround
		case 'D':
			t.state.lineFeed()
			p.state = termGround
		case 'E':
			t.state.col = 0
			t.state.lineFeed()
			p.state = termGround
		case 'M':
			t.state.reverseIndex()
			p.state = termGround
		case 'c':
			t.resetScreen()
			p.state = termGround
		case '(', ')', '*', '+', '#', '%':
			p.state = termCharset
		default:
			p.state = termGround
		}
	case termCharset:
		p.state = termGround
	case termOSC:
		if b == 0x07 {
			p.state = termGround
		} else if b == 0x1b {
			p.state = termOSCEsc
		}
	case termOSCEsc:
		if b == '\\' {
			p.state = termGround
		} else {
			p.state = termOSC
		}
	case termCSI:
		switch {
		case b >= '0' && b <= '9':
			p.cur = p.cur*10 + int(b-'0')
			p.hasCur = true
		case b == ';' || b == ':':
			p.pushParam()
		case b == '?' && len(p.params) == 0 && !p.hasCur:
			p.private = true
		case b == ' ' || b == '!':
			// an intermediate byte before the final, ignored
		case b >= 0x40 && b <= 0x7e:
			if p.hasCur {
				p.pushParam()
			} else if len(p.params) == 0 {
				p.params = append(p.params, 0)
			}
			t.executeCSI(b)
			p.reset()
		}
	}
}

func (p *terminalParser) param(i, def int) int {
	if i < len(p.params) && p.params[i] != 0 {
		return p.params[i]
	}
	return def
}

func (t *Terminal) executeCSI(final byte) {
	p := &t.parser
	s := &t.state
	if p.private && (final == 'h' || final == 'l') {
		t.setPrivateMode(p.params, final == 'h')
		return
	}
	switch final {
	case 'A':
		s.row -= p.param(0, 1)
		s.clampCursor()
	case 'B', 'e':
		s.row += p.param(0, 1)
		s.clampCursor()
	case 'C', 'a':
		s.col += p.param(0, 1)
		s.clampCursor()
	case 'D':
		s.col -= p.param(0, 1)
		s.clampCursor()
	case 'E':
		s.row += p.param(0, 1)
		s.col = 0
		s.clampCursor()
	case 'F':
		s.row -= p.param(0, 1)
		s.col = 0
		s.clampCursor()
	case 'G', '`':
		s.col = p.param(0, 1) - 1
		s.clampCursor()
	case 'd':
		s.row = p.param(0, 1) - 1
		s.clampCursor()
	case 'H', 'f':
		s.row = p.param(0, 1) - 1
		s.col = p.param(1, 1) - 1
		s.clampCursor()
	case 'J':
		s.eraseDisplay(p.param(0, 0))
	case 'K':
		s.eraseLine(p.param(0, 0))
	case 'm':
		t.sgr(p.params)
	case 'r':
		top := p.param(0, 1) - 1
		bottom := p.param(1, s.rows) - 1
		if top < 0 {
			top = 0
		}
		if bottom >= s.rows {
			bottom = s.rows - 1
		}
		if top < bottom {
			s.top, s.bottom = top, bottom
			s.row, s.col = top, 0
			s.wrap = false
		}
	case 's':
		s.savedR, s.savedC = s.row, s.col
	case 'u':
		s.row, s.col = s.savedR, s.savedC
		s.wrap = false
	case 'L':
		s.insertLines(p.param(0, 1))
	case 'M':
		s.deleteLines(p.param(0, 1))
	case 'P':
		s.deleteChars(p.param(0, 1))
	case '@':
		s.insertChars(p.param(0, 1))
	case 'X':
		n := p.param(0, 1)
		for c := s.col; c < s.col+n && c < s.cols; c++ {
			s.lines[s.row][c] = termCell{r: ' '}
		}
	case 'S':
		s.scrollUp(p.param(0, 1))
	case 'T':
		s.scrollDown(p.param(0, 1))
	}
}

func (t *Terminal) setPrivateMode(params []int, on bool) {
	for _, n := range params {
		switch n {
		case 25:
			t.state.cursor = on
		case 47, 1047, 1049:
			if on {
				t.enterAlt()
			} else {
				t.exitAlt()
			}
		case 7:
			t.state.autowrap = on
		}
	}
}

func (s *terminalState) clampCursor() {
	if s.row < 0 {
		s.row = 0
	}
	if s.row >= s.rows {
		s.row = s.rows - 1
	}
	if s.col < 0 {
		s.col = 0
	}
	if s.col >= s.cols {
		s.col = s.cols - 1
	}
	s.wrap = false
}

func (s *terminalState) lineFeed() {
	if s.row >= s.bottom {
		s.scrollUp(1)
	} else if s.row < s.rows-1 {
		s.row++
	}
	s.wrap = false
}

func (s *terminalState) reverseIndex() {
	if s.row <= s.top {
		s.scrollDown(1)
	} else if s.row > 0 {
		s.row--
	}
	s.wrap = false
}

func (t *Terminal) put(r rune) {
	s := &t.state
	if s.wrap {
		s.lineFeed()
		s.col = 0
		s.wrap = false
	}
	if s.col >= s.cols {
		s.col = 0
		s.lineFeed()
	}
	cell := s.cell(s.row, s.col)
	cell.r = r
	cell.fg, cell.bg, cell.attr = t.fg, t.bg, t.attr
	s.col++
	if s.col >= s.cols {
		s.col = s.cols - 1
		s.wrap = s.autowrap
	}
}

func (s *terminalState) eraseLine(mode int) {
	switch mode {
	case 0:
		for c := s.col; c < s.cols; c++ {
			s.lines[s.row][c] = termCell{r: ' '}
		}
	case 1:
		for c := 0; c <= s.col && c < s.cols; c++ {
			s.lines[s.row][c] = termCell{r: ' '}
		}
	default:
		s.clearLine(s.row)
	}
}

func (s *terminalState) eraseDisplay(mode int) {
	switch mode {
	case 0:
		s.eraseLine(0)
		s.eraseRows(s.row+1, s.rows-1)
	case 1:
		s.eraseLine(1)
		s.eraseRows(0, s.row-1)
	case 2:
		s.eraseRows(0, s.rows-1)
	case 3:
		s.scroll = nil
	}
}

func (s *terminalState) insertLines(n int) {
	if s.row < s.top || s.row > s.bottom {
		return
	}
	if n > s.bottom-s.row+1 {
		n = s.bottom - s.row + 1
	}
	copy(s.lines[s.row+n:s.bottom+1], s.lines[s.row:s.bottom+1-n])
	for i := 0; i < n; i++ {
		s.lines[s.row+i] = blankTermLine(s.cols)
	}
}

func (s *terminalState) deleteLines(n int) {
	if s.row < s.top || s.row > s.bottom {
		return
	}
	if n > s.bottom-s.row+1 {
		n = s.bottom - s.row + 1
	}
	copy(s.lines[s.row:s.bottom+1-n], s.lines[s.row+n:s.bottom+1])
	for i := 0; i < n; i++ {
		s.lines[s.bottom-i] = blankTermLine(s.cols)
	}
}

func (s *terminalState) insertChars(n int) {
	if n < 1 || s.col >= s.cols {
		return
	}
	if n > s.cols-s.col {
		n = s.cols - s.col
	}
	line := s.lines[s.row]
	copy(line[s.col+n:], line[s.col:s.cols-n])
	for c := s.col; c < s.col+n; c++ {
		line[c] = termCell{r: ' '}
	}
}

func (s *terminalState) deleteChars(n int) {
	if n < 1 || s.col >= s.cols {
		return
	}
	if n > s.cols-s.col {
		n = s.cols - s.col
	}
	line := s.lines[s.row]
	copy(line[s.col:], line[s.col+n:])
	for c := s.cols - n; c < s.cols; c++ {
		line[c] = termCell{r: ' '}
	}
}

// resetScreen clears the current screen and the cursor style.
func (t *Terminal) resetScreen() {
	t.state = newTerminalState(t.state.cols, t.state.rows)
	t.state.maxBack = t.maxBack()
	t.state.cursor, t.state.autowrap = true, true
	t.fg, t.bg, t.attr = 0, 0, 0
	t.scroll = 0
}

// enterAlt switches to a clean alternate screen, keeping the shell's screen
// and cursor for exitAlt.
func (t *Terminal) enterAlt() {
	if t.alt {
		return
	}
	t.normal = t.state
	t.state = newTerminalState(t.state.cols, t.state.rows)
	t.state.maxBack = t.maxBack()
	t.state.altState = true
	t.state.setAlt(true)
	t.state.cursor, t.state.autowrap = true, true
	t.alt = true
}

func (t *Terminal) exitAlt() {
	if !t.alt {
		return
	}
	t.state = t.normal
	t.state.altState = false
	t.alt = false
	t.state.cursor = true
}

// sgr applies a Select Graphic Rendition list to the current pen.
func (t *Terminal) sgr(params []int) {
	if len(params) == 0 {
		params = []int{0}
	}
	for i := 0; i < len(params); i++ {
		p := params[i]
		switch {
		case p == 0:
			t.fg, t.bg, t.attr = 0, 0, 0
		case p == 1:
			t.attr |= termBold
		case p == 2:
			t.attr |= termFaint
		case p == 3:
			t.attr |= termItalic
		case p == 4:
			t.attr |= termUnderline
		case p == 7:
			t.attr |= termReverse
		case p == 8:
			t.attr |= termHidden
		case p == 9:
			t.attr |= termStrike
		case p == 22:
			t.attr &^= termBold | termFaint
		case p == 23:
			t.attr &^= termItalic
		case p == 24:
			t.attr &^= termUnderline
		case p == 27:
			t.attr &^= termReverse
		case p == 29:
			t.attr &^= termStrike
		case p == 39:
			t.fg = 0
		case p == 49:
			t.bg = 0
		case p >= 30 && p <= 37:
			t.fg = ansiColor(p - 30)
		case p >= 90 && p <= 97:
			t.fg = ansiColor(p - 90 + 8)
		case p >= 40 && p <= 47:
			t.bg = ansiColor(p - 40)
		case p >= 100 && p <= 107:
			t.bg = ansiColor(p - 100 + 8)
		case p == 38 || p == 48:
			c, used := ansiExtended(params[i+1:])
			if p == 38 {
				t.fg = c
			} else {
				t.bg = c
			}
			i += used
		}
	}
}

// ansiExtended reads `5;n` or `2;r;g;b` after 38/48 and returns the colour and
// how many parameters it used.
func ansiExtended(rest []int) (uint32, int) {
	if len(rest) == 0 {
		return 0, 0
	}
	switch rest[0] {
	case 5:
		if len(rest) >= 2 {
			return xterm256(rest[1]), 2
		}
		return 0, 1
	case 2:
		if len(rest) >= 4 {
			return rgb(rest[1], rest[2], rest[3]), 4
		}
		return 0, len(rest)
	}
	return 0, 0
}

func rgb(r, g, b int) uint32 {
	clamp := func(v int) uint32 {
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint32(v)
	}
	return clamp(r)<<24 | clamp(g)<<16 | clamp(b)<<8 | 0xff
}

var ansiPalette = [16]uint32{
	0x000000ff, 0xcd0000ff, 0x00cd00ff, 0xcdcd00ff,
	0x0000eeff, 0xcd00cdff, 0x00cdcdff, 0xe5e5e5ff,
	0x7f7f7fff, 0xff0000ff, 0x00ff00ff, 0xffff00ff,
	0x5c5cffff, 0xff00ffff, 0x00ffffff, 0xffffffff,
}

func ansiColor(n int) uint32 {
	if n < 0 || n >= len(ansiPalette) {
		return 0
	}
	return ansiPalette[n]
}

func xterm256(n int) uint32 {
	if n < 16 {
		return ansiColor(n)
	}
	if n >= 232 {
		v := 8 + (n-232)*10
		return rgb(v, v, v)
	}
	n -= 16
	r, g, b := n/36, (n/6)%6, n%6
	level := func(v int) int {
		if v == 0 {
			return 0
		}
		return 55 + v*40
	}
	return rgb(level(r), level(g), level(b))
}

// Key sends one non-text key to the writer. It reports whether the terminal
// consumed it. Text runes arrive through WriteInput from the input layer.
func (t *Terminal) Key(k Key, m Mods) bool {
	ctrl := m.Control()
	if ctrl {
		switch {
		case k >= KeyA && k <= KeyA+25:
			t.WriteInput(string(rune(1 + int(k-KeyA))))
			return true
		case k == KeySpace:
			t.WriteInput("\x00")
			return true
		}
	}
	var out string
	switch k {
	case KeyEnter:
		out = "\r"
	case KeyBackspace:
		out = "\x7f"
	case KeyTab:
		if m.Shift {
			out = "\x1b[Z"
		} else {
			out = "\t"
		}
	case KeyEscape:
		out = "\x1b"
	case KeyDelete:
		out = "\x1b[3~"
	case KeyHome:
		out = "\x1b[H"
	case KeyEnd:
		out = "\x1b[F"
	case KeyUp:
		out = "\x1b[A"
	case KeyDown:
		out = "\x1b[B"
	case KeyRight:
		out = "\x1b[C"
	case KeyLeft:
		out = "\x1b[D"
	case KeySpace:
		out = " "
	default:
		return false
	}
	t.WriteInput(out)
	return true
}
