package tree

import (
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/goi-gui/goi/internal/img"
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
	// Exit fires once when the reader given to SetReader ends. A shell ends
	// there after `exit` or Ctrl+D, so an app can close its window from it.
	Exit Event[struct{}]

	mu      sync.Mutex
	exited  bool
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
	// lastKey suppresses the text-input copy of a printable key: some
	// platforms deliver a letter as both KeyA and Char, others only as KeyA.
	lastKey   rune
	lastKeyAt time.Time
	// Cell size is written by the painter; input uses it to map a pointer to a
	// cell. Selection is in view coordinates, so it stays on the rows the
	// reader dragged over.
	cellW, cellH float64
	selA, selB   termPoint
	selActive    bool
	mouseMode    int
	mouseSGR     bool
	mouseUTF8    bool
	mouseURXVT   bool
	bracketed    bool
	// link is the current OSC 8 target, written into every cell put draws.
	link string
}

type termPoint struct{ row, col int }

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
// ends at EOF and emits Exit; replacing it does not close the previous reader.
func (t *Terminal) SetReader(r io.Reader) {
	t.mu.Lock()
	t.reader = r
	t.exited = false
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
				t.readerEnded(tr, r)
				return
			}
		}
	}()
}

// readerEnded emits Exit once, on the UI goroutine, for the current reader.
func (t *Terminal) readerEnded(tr *Tree, r io.Reader) {
	t.mu.Lock()
	if t.reader != r || t.exited {
		t.mu.Unlock()
		return
	}
	t.exited = true
	t.mu.Unlock()
	emit := func() { t.Exit.Emit(struct{}{}) }
	if tr != nil {
		tr.Post(emit)
		return
	}
	emit()
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

// takeKeyRune drops the Char event that follows a printable Key event on
// platforms that deliver both; a rune with no preceding key is left alone.
func (t *Terminal) takeKeyRune(r rune) bool {
	if t.lastKey == r && time.Since(t.lastKeyAt) < 100*time.Millisecond {
		t.lastKey = 0
		return true
	}
	return false
}

// WriteRune sends one typed rune and drops the duplicate a platform may report
// after a printable Key event.
func (t *Terminal) WriteRune(r rune) {
	if t.takeKeyRune(r) {
		return
	}
	t.WriteInput(string(r))
}

// WriteText sends committed text. A one-rune commit that follows its printable
// Key event is the same character arriving twice, so it is dropped.
func (t *Terminal) WriteText(s string) {
	if r, size := utf8.DecodeRuneInString(s); size == len(s) && t.takeKeyRune(r) {
		return
	}
	t.WriteInput(s)
}

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

// OnExit subscribes to the end of the reader, after the shell exits or the
// stream closes. The callback runs on the UI goroutine.
func (t *Terminal) OnExit(fn func()) (off func()) {
	return t.Exit.On(func(struct{}) { fn() })
}

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
	lines     []termLine
	scroll    []termLine
	rows      int
	cols      int
	row       int
	col       int
	savedR    int
	savedC    int
	top       int
	bottom    int
	wrap      bool
	maxBack   int
	altState  bool
	cursor    bool
	autowrap  bool
	origin    bool
	appCursor bool
	blink     bool
	focus     bool
}

type termLine []termCell

type termCell struct {
	r    rune
	fg   uint32
	bg   uint32
	attr termAttr
	// extra holds the rare per-cell data: combining marks, a hyperlink, the
	// second half of a wide character, or a sixel bitmap. Keeping it behind a
	// pointer keeps a scrollback line of plain text small.
	extra *termCellExtra
}

type termCellExtra struct {
	comb  string
	link  string
	cont  bool
	image *img.Data
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
// default colour. Comb is attached to R, Cont marks the second half of a wide
// character, and Image is a sixel bitmap anchored here.
type TerminalCell struct {
	R                 rune
	Comb              string
	Cont              bool
	Image             *img.Data
	Link              string
	FG, BG            uint32
	Bold, Faint       bool
	Italic, Underline bool
	Reverse, Strike   bool
	Hidden            bool
}

// TerminalView is the visible grid plus the cursor, with scrollback applied.
type TerminalView struct {
	Lines       [][]TerminalCell
	CursorRow   int
	CursorCol   int
	CursorOn    bool
	CursorBlink bool
	Cols, Rows  int
}

// View returns the rows a painter should draw.
func (t *Terminal) View(rows int) TerminalView {
	t.mu.Lock()
	defer t.mu.Unlock()
	if rows < 1 {
		rows = t.state.rows
	}
	v := TerminalView{Rows: rows, Cols: t.state.cols, CursorRow: -1, CursorCol: -1,
		CursorOn: t.state.cursor, CursorBlink: t.state.blink}
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
				tc := TerminalCell{
					R: cell.r, FG: cell.fg, BG: cell.bg,
					Bold: cell.attr&termBold != 0, Faint: cell.attr&termFaint != 0,
					Italic: cell.attr&termItalic != 0, Underline: cell.attr&termUnderline != 0,
					Reverse: cell.attr&termReverse != 0, Strike: cell.attr&termStrike != 0,
					Hidden: cell.attr&termHidden != 0,
				}
				if ex := cell.extra; ex != nil {
					tc.Comb, tc.Link, tc.Cont, tc.Image = ex.comb, ex.link, ex.cont, ex.image
				}
				line[c] = tc
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

// Fit sets the character grid measured from the painted box.
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
	t.selActive = false
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

// handleDCS reads a complete device-control string. A sixel sequence is the
// one it can draw; other DCS payloads are ignored.
func (t *Terminal) handleDCS(s string) {
	if i := strings.IndexByte(s, 'q'); i >= 0 {
		t.putSixel(s[i+1:])
	}
}

// maxSixelDim bounds a sixel bitmap: a malformed sequence must not ask for a
// gigabyte of pixels.
const maxSixelDim = 4096

// putSixel decodes the payload of a sixel DCS into a bitmap and anchors it at
// the cursor cell.
func (t *Terminal) putSixel(data string) {
	var rows [][]uint32
	var maxW int
	ensure := func(y, x int) bool {
		if y < 0 || x < 0 || y >= maxSixelDim || x >= maxSixelDim {
			return false
		}
		for len(rows) <= y {
			rows = append(rows, nil)
		}
		if len(rows[y]) <= x {
			rows[y] = append(rows[y], make([]uint32, x+1-len(rows[y]))...)
		}
		return true
	}
	var palette [256]uint32
	var have [256]bool
	color := 0
	set := func(x, y int) {
		if !ensure(y, x) {
			return
		}
		c := palette[color]
		if !have[color] {
			c = xterm256(color)
			palette[color], have[color] = c, true
		}
		rows[y][x] = c
	}
	x, y, repeat := 0, 0, 1
	for i := 0; i < len(data); i++ {
		ch := data[i]
		switch {
		case ch >= '?' && ch <= '~':
			bits := ch - '?'
			for r := 0; r < repeat; r++ {
				for b := 0; b < 6; b++ {
					if bits&(1<<b) != 0 {
						set(x+r, y+b)
					}
				}
			}
			x += repeat
			repeat = 1
			if x > maxW {
				maxW = x
			}
		case ch == '!':
			n, next := sixelNumber(data, i+1)
			if next > i+1 {
				repeat = n
				i = next - 1
			}
			if repeat < 1 {
				repeat = 1
			}
		case ch == '#':
			n, next := sixelNumber(data, i+1)
			if n >= 0 && n < len(palette) {
				color = n
			}
			if next < len(data) && data[next] == ';' {
				var vals []int
				j := next + 1
				for len(vals) < 4 && j < len(data) && data[j] >= '0' && data[j] <= '9' {
					v, end := sixelNumber(data, j)
					vals = append(vals, v)
					if end < len(data) && data[end] == ';' {
						j = end + 1
						continue
					}
					j = end
					break
				}
				if len(vals) == 4 {
					palette[color] = sixelColor(vals[0], vals[1], vals[2], vals[3])
					have[color] = true
				}
				i = j - 1
			} else {
				i = next - 1
			}
		case ch == '$':
			x = 0
		case ch == '-':
			x, y = 0, y+6
		case ch == '"':
			// Raster attributes: Pan;Pad;Ph;Pv. Ph/Pv declare the size, which
			// keeps the bitmap honest when a band is short.
			j := i + 1
			var vals []int
			for len(vals) < 4 && j < len(data) {
				if data[j] == ';' {
					j++
					continue
				}
				if data[j] < '0' || data[j] > '9' {
					break
				}
				v, end := sixelNumber(data, j)
				vals = append(vals, v)
				j = end
			}
			if len(vals) >= 4 {
				if vals[2] > maxW && vals[2] <= maxSixelDim {
					maxW = vals[2]
				}
				if vals[3] > 0 && vals[3] <= maxSixelDim {
					for len(rows) < vals[3] {
						rows = append(rows, nil)
					}
				}
			}
			i = j - 1
		}
	}
	for len(rows) > 0 && len(rows[len(rows)-1]) == 0 {
		rows = rows[:len(rows)-1]
	}
	if maxW == 0 {
		for _, row := range rows {
			if len(row) > maxW {
				maxW = len(row)
			}
		}
	}
	if maxW == 0 || len(rows) == 0 {
		return
	}
	h := len(rows)
	pix := make([]byte, maxW*h*4)
	for yy, row := range rows {
		for xx, v := range row {
			if v == 0 {
				continue
			}
			o := (yy*maxW + xx) * 4
			pix[o], pix[o+1], pix[o+2], pix[o+3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
		}
	}
	t.placeImage(&img.Data{W: maxW, H: h, Pix: pix})
}

// placeImage anchors a bitmap at the cursor cell, where a sixel sequence put it.
func (t *Terminal) placeImage(d *img.Data) {
	s := &t.state
	if s.col >= s.cols {
		s.col = s.cols - 1
	}
	cell := s.cell(s.row, s.col)
	cell.r = ' '
	if cell.extra == nil {
		cell.extra = &termCellExtra{}
	}
	cell.extra.image = d
}

func sixelNumber(s string, i int) (int, int) {
	n := 0
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
	}
	if i == start {
		return 0, i
	}
	return n, i
}

// sixelColor turns a sixel colour spec into RGB: space 2 is 0..100 RGB, space
// 1 is 0..360/0..100/0..100 HLS.
func sixelColor(space, a, b, c int) uint32 {
	if space == 1 {
		return hsl(int(a)%360, clampPct(b), clampPct(c))
	}
	return rgb(clampPct(a)*255/100, clampPct(b)*255/100, clampPct(c)*255/100)
}

func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// hsl converts a sixel HLS triple, with H in degrees and S/L in percent.
func hsl(h, s, l int) uint32 {
	hf := float64(h) / 360
	sf, lf := float64(s)/100, float64(l)/100
	var r, g, b float64
	if sf == 0 {
		r, g, b = lf, lf, lf
	} else {
		q := lf * (1 + sf)
		if lf >= 0.5 {
			q = lf + sf - lf*sf
		}
		p := 2*lf - q
		conv := func(t float64) float64 {
			if t < 0 {
				t++
			}
			if t > 1 {
				t--
			}
			switch {
			case t < 1.0/6:
				return p + (q-p)*6*t
			case t < 0.5:
				return q
			case t < 2.0/3:
				return p + (q-p)*(2.0/3-t)*6
			}
			return p
		}
		r, g, b = conv(hf+1.0/3), conv(hf), conv(hf-1.0/3)
	}
	return rgb(int(math.Round(r*255)), int(math.Round(g*255)), int(math.Round(b*255)))
}

// terminalParser is the byte state machine for escape sequences.
type terminalParser struct {
	state   termParseState
	params  []int
	cur     int
	hasCur  bool
	private bool
	// str accumulates an OSC or DCS payload, and kind says which kind it is.
	str  []byte
	kind byte
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

// finishString hands a completed OSC or DCS payload to the terminal.
// strLimit caps a string payload: a title OSC is small, but a sixel bitmap is
// legitimately large, and neither may grow without bound.
func (p *terminalParser) strLimit() int {
	if p.kind == 'P' {
		return 8 << 20
	}
	return 8 << 10
}

func (p *terminalParser) finishString(t *Terminal) {
	switch p.kind {
	case ']':
		t.handleOSC(string(p.str))
	case 'P':
		t.handleDCS(string(p.str))
	}
	p.str = p.str[:0]
	p.kind = 0
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
			p.state, p.kind = termOSC, ']'
			p.str = p.str[:0]
		case 'P':
			p.state, p.kind = termOSC, 'P'
			p.str = p.str[:0]
		case 'X', '^', '_':
			p.state, p.kind = termOSC, 'x'
			p.str = p.str[:0]
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
		if b == 0x07 && p.kind == ']' {
			p.finishString(t)
			p.state = termGround
		} else if b == 0x1b {
			p.state = termOSCEsc
		} else if len(p.str) < p.strLimit() {
			p.str = append(p.str, b)
		}
	case termOSCEsc:
		if b == '\\' {
			p.finishString(t)
			p.state = termGround
		} else {
			p.str = append(p.str, 0x1b, b)
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
		if s.origin {
			s.row += s.top
		}
		s.clampCursor()
	case 'H', 'f':
		s.row = p.param(0, 1) - 1
		if s.origin {
			s.row += s.top
		}
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
		case 1:
			t.state.appCursor = on
		case 6:
			t.state.origin = on
			if on {
				t.state.row, t.state.col = t.state.top, 0
			} else {
				t.state.row, t.state.col = 0, 0
			}
		case 7:
			t.state.autowrap = on
		case 9:
			if on {
				t.mouseMode = n
			} else if t.mouseMode == n {
				t.mouseMode = 0
			}
		case 12:
			t.state.blink = on
		case 1000, 1002, 1003:
			if on {
				t.mouseMode = n
			} else if t.mouseMode == n {
				t.mouseMode = 0
			}
		case 1004:
			t.state.focus = on
		case 1005:
			t.mouseUTF8 = on
		case 1006:
			t.mouseSGR = on
		case 1015:
			t.mouseURXVT = on
		case 1048:
			if on {
				t.state.savedR, t.state.savedC = t.state.row, t.state.col
			} else {
				t.state.row, t.state.col = t.state.savedR, t.state.savedC
				t.state.wrap = false
			}
		case 2004:
			t.bracketed = on
		}
	}
}

func (s *terminalState) clampCursor() {
	low, high := 0, s.rows-1
	if s.origin {
		low, high = s.top, s.bottom
	}
	if s.row < low {
		s.row = low
	}
	if s.row > high {
		s.row = high
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
	if termRuneWidth(r) == 0 {
		t.combine(r)
		return
	}
	s := &t.state
	if s.wrap {
		s.lineFeed()
		s.col = 0
		s.wrap = false
	}
	wide := termRuneWidth(r) == 2
	if wide && s.col == s.cols-1 {
		// A wide character cannot straddle a line: wrap first.
		s.lineFeed()
		s.col = 0
		s.wrap = false
	}
	if s.col >= s.cols {
		s.col = 0
		s.lineFeed()
	}
	t.writeCell(s.cell(s.row, s.col), r)
	s.col++
	if wide && s.col < s.cols {
		c := s.cell(s.row, s.col)
		*c = termCell{fg: t.fg, bg: t.bg, attr: t.attr}
		c.extra = &termCellExtra{cont: true, link: t.link}
		s.col++
	}
	if s.col >= s.cols {
		s.col = s.cols - 1
		s.wrap = s.autowrap
	}
}

// writeCell sets a base cell's glyph and the current pen.
func (t *Terminal) writeCell(cell *termCell, r rune) {
	*cell = termCell{r: r, fg: t.fg, bg: t.bg, attr: t.attr}
	if t.link != "" {
		cell.extra = &termCellExtra{link: t.link}
	}
}

// combine attaches a zero-width mark to the character before the cursor.
func (t *Terminal) combine(r rune) {
	s := &t.state
	col := s.col - 1
	if s.wrap {
		col = s.col
	}
	row := s.row
	for col > 0 && s.cell(row, col).isCont() {
		col--
	}
	if col < 0 || col >= s.cols {
		return
	}
	cell := s.cell(row, col)
	if cell.r == 0 && cell.extra == nil {
		return
	}
	if cell.extra == nil {
		cell.extra = &termCellExtra{}
	}
	cell.extra.comb += string(r)
}

func (c *termCell) isCont() bool { return c.extra != nil && c.extra.cont }

// termRuneWidth is the number of cells a rune occupies: 0 for a combining
// mark, 2 for an East Asian wide character, 1 otherwise.
func termRuneWidth(r rune) int {
	switch {
	case r == 0 || r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r):
		return 0
	case r == 0x200B || r == 0x200C || r == 0x200D:
		return 0
	case r >= 0xFE00 && r <= 0xFE0F:
		return 0
	case r >= 0xE0100 && r <= 0xE01EF:
		return 0
	case isWideRune(r):
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	if r < 0x1100 {
		return false
	}
	return r <= 0x115F ||
		r == 0x2329 || r == 0x232A ||
		(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
		(r >= 0xAC00 && r <= 0xD7A3) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE10 && r <= 0xFE19) ||
		(r >= 0xFE30 && r <= 0xFE6F) ||
		(r >= 0xFF00 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x1F300 && r <= 0x1FAFF) ||
		(r >= 0x20000 && r <= 0x3FFFD)
}

// handleOSC reads a complete OSC payload. The only one the terminal acts on is
// OSC 8, which sets the hyperlink target for following cells.
func (t *Terminal) handleOSC(s string) {
	if !strings.HasPrefix(s, "8;") {
		return
	}
	rest := s[2:]
	if i := strings.IndexByte(rest, ';'); i >= 0 {
		t.link = rest[i+1:]
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
	t.link = ""
	t.mouseMode, t.mouseSGR, t.mouseUTF8, t.mouseURXVT = 0, false, false, false
	t.bracketed = false
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

// SetCellSize records the painted cell metrics, so a pointer can find a cell.
func (t *Terminal) SetCellSize(w, h float64) {
	t.mu.Lock()
	t.cellW, t.cellH = w, h
	t.mu.Unlock()
}

// CellSize is the painted cell metrics.
func (t *Terminal) CellSize() (float64, float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cellW, t.cellH
}

// SelectionStart clears the old range and anchors a new one.
func (t *Terminal) SelectionStart(row, col int) {
	t.mu.Lock()
	t.selA = termPoint{row, col}
	t.selB = t.selA
	t.selActive = true
	t.mu.Unlock()
	t.MarkDirty(DirtyPaint)
}

// SelectionExtend moves the loose end of the range.
func (t *Terminal) SelectionExtend(row, col int) {
	t.mu.Lock()
	if !t.selActive {
		t.mu.Unlock()
		return
	}
	t.selB = termPoint{row, col}
	t.mu.Unlock()
	t.MarkDirty(DirtyPaint)
}

// ClearSelection drops the range.
func (t *Terminal) ClearSelection() {
	t.mu.Lock()
	t.selActive = false
	t.mu.Unlock()
	t.MarkDirty(DirtyPaint)
}

// Selection returns the normalised range in view coordinates.
func (t *Terminal) Selection() (startRow, startCol, endRow, endCol int, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.selActive {
		return 0, 0, 0, 0, false
	}
	a, b := t.selA, t.selB
	if a.row > b.row || (a.row == b.row && a.col > b.col) {
		a, b = b, a
	}
	return a.row, a.col, b.row, b.col, true
}

// SelectedText is the selected cells as plain text, one line per row.
func (t *Terminal) SelectedText(rows int) string {
	t.mu.Lock()
	if !t.selActive {
		t.mu.Unlock()
		return ""
	}
	a, b := t.selA, t.selB
	if a.row > b.row || (a.row == b.row && a.col > b.col) {
		a, b = b, a
	}
	t.mu.Unlock()
	view := t.View(rows)
	var lines []string
	for row := a.row; row <= b.row && row < len(view.Lines); row++ {
		if row < 0 {
			continue
		}
		from, to := 0, len(view.Lines[row])
		if row == a.row {
			from = termClamp(a.col, to)
		}
		if row == b.row {
			to = termClamp(b.col+1, to)
		}
		var b strings.Builder
		for _, cell := range view.Lines[row][from:to] {
			if cell.Cont {
				continue
			}
			if cell.R == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(cell.R)
			b.WriteString(cell.Comb)
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(lines, "\n")
}

// LinkAt returns the OSC 8 target at a view cell, or "". It reads the cell
// directly so pointer motion does not allocate a whole view.
func (t *Terminal) LinkAt(rows, row, col int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	total := len(t.state.scroll) + len(t.state.lines)
	end := total
	if t.scroll != 0 {
		end = total - t.scroll
	}
	idx := end - rows + row
	if idx < 0 || idx >= total {
		return ""
	}
	var line termLine
	if idx < len(t.state.scroll) {
		line = t.state.scroll[idx]
	} else if idx-len(t.state.scroll) < len(t.state.lines) {
		line = t.state.lines[idx-len(t.state.scroll)]
	}
	if col < 0 || col >= len(line) {
		return ""
	}
	cell := line[col]
	if cell.isCont() && col > 0 {
		cell = line[col-1]
	}
	if cell.extra == nil {
		return ""
	}
	return cell.extra.link
}

// WriteFocus reports window focus when the program enabled mode 1004.
func (t *Terminal) WriteFocus(focused bool) {
	t.mu.Lock()
	report := t.state.focus
	t.mu.Unlock()
	if !report {
		return
	}
	if focused {
		t.WriteInput("\x1b[I")
	} else {
		t.WriteInput("\x1b[O")
	}
}

// MouseReporting reports whether the running program asked for mouse events.
func (t *Terminal) MouseReporting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mouseMode != 0 || t.mouseUTF8 || t.mouseURXVT
}

// MouseMode returns the enabled DEC mouse mode, 0 when off.
func (t *Terminal) MouseMode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mouseMode
}

// WriteMouse sends one mouse event in the mode the program enabled.
func (t *Terminal) WriteMouse(button, row, col int, press, motion bool) {
	t.mu.Lock()
	mode, sgr, utf8mode, urxvt := t.mouseMode, t.mouseSGR, t.mouseUTF8, t.mouseURXVT
	t.mu.Unlock()
	// The UTF-8 and urxvt encodings also enable reporting on their own.
	if mode == 0 && !utf8mode && !urxvt {
		return
	}
	if mode == 9 && (!press || motion) {
		return
	}
	if motion && mode != 1003 && !press {
		return
	}
	if row < 0 || col < 0 {
		return
	}
	b := button
	if motion {
		b += 32
	}
	if sgr {
		final := "M"
		if !press {
			final = "m"
		}
		t.WriteInput(fmt.Sprintf("\x1b[<%d;%d;%d%s", b, col+1, row+1, final))
		return
	}
	// X10 reports a release as button 3.
	if !press && !motion {
		b = 3
	}
	if urxvt {
		t.WriteInput(fmt.Sprintf("\x1b[%d;%d;%dM", 32+b, col+1, row+1))
		return
	}
	if utf8mode {
		t.WriteInput("\x1b[M" + string(rune(32+b)) + string(rune(32+col+1)) + string(rune(32+row+1)))
		return
	}
	if b+32 > 255 || row+33 > 255 || col+33 > 255 {
		return
	}
	t.WriteInput("\x1b[M" + string([]byte{byte(32 + b), byte(32 + col + 1), byte(32 + row + 1)}))
}

// WritePaste sends pasted text, wrapped when the program enabled bracketed
// paste.
func (t *Terminal) WritePaste(s string) {
	t.mu.Lock()
	bracketed := t.bracketed
	t.mu.Unlock()
	if bracketed {
		t.WriteInput("\x1b[200~" + s + "\x1b[201~")
		return
	}
	t.WriteInput(s)
}

// Key sends one non-text key to the writer. It reports whether the terminal
// consumed it. Text runes arrive through WriteInput from the input layer.
func (t *Terminal) Key(k Key, m Mods) bool {
	ctrl := m.Control()
	if ctrl {
		switch {
		case k >= KeyA && k <= KeyZ:
			t.WriteInput(string(rune(1 + int(k-KeyA))))
			return true
		case k == KeySpace:
			t.WriteInput("\x00")
			return true
		}
	}
	if !ctrl && k >= KeyA && k <= KeyZ {
		r := rune('a' + (k - KeyA))
		if m.Shift {
			r -= 'a' - 'A'
		}
		t.lastKey, t.lastKeyAt = r, time.Now()
		t.WriteInput(string(r))
		return true
	}
	t.mu.Lock()
	appCursor := t.state.appCursor
	t.mu.Unlock()
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
		if appCursor {
			out = "\x1bOH"
		}
	case KeyEnd:
		out = "\x1b[F"
		if appCursor {
			out = "\x1bOF"
		}
	case KeyUp:
		out = "\x1b[A"
		if appCursor {
			out = "\x1bOA"
		}
	case KeyDown:
		out = "\x1b[B"
		if appCursor {
			out = "\x1bOB"
		}
	case KeyRight:
		out = "\x1b[C"
		if appCursor {
			out = "\x1bOC"
		}
	case KeyLeft:
		out = "\x1b[D"
		if appCursor {
			out = "\x1bOD"
		}
	case KeySpace:
		out = " "
	default:
		return false
	}
	t.WriteInput(out)
	return true
}

func termClamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}
