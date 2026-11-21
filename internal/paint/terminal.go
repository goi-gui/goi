package paint

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// paintTerminal draws the character grid, ANSI backgrounds, attributes and the
// cursor. The terminal owns the parser; paint only reads its view.
func (l *List) paintTerminal(c *tree.Component, r tree.Rect, clip tree.Rect) {
	term, ok := c.Node().(*tree.Terminal)
	if !ok {
		return
	}
	col := l.Theme.Colors
	bg := col.SurfaceContainerLowest
	if v := c.Base().Background(); v != 0 {
		bg = v
	}
	fg := col.OnSurface
	if v := term.Foreground.Get(); v != 0 {
		fg = v
	}
	radius := 0.0
	if c.Base().RadiusSet() {
		radius = c.Base().Radii()[0]
	}
	l.fillRect(r, bg, radius, 0, 0, clip)

	// Terminal content is monospace. A design can choose another family, but
	// the default is the embedded face so columns line up.
	family := l.family
	if family == "" {
		l.family = "Roboto Mono"
	}
	defer func() { l.family = family }()

	size := term.FontSize.Get()
	if size <= 0 {
		size = 13
	}
	st := m3.RoleStyle("bodyMedium")
	st.Size = size
	st.LineHeight = size * 1.25
	cellW := textWidth(l, "M", st)
	if cellW <= 0 {
		cellW = size * 0.6
	}
	cellH := st.LineHeight
	if cellH <= 0 {
		cellH = size * 1.25
	}
	pad := c.Base().PaddingEdges()
	inner := tree.Rect{
		X: r.X + pad.Left,
		Y: r.Y + pad.Top,
		W: maxf(r.W-pad.Left-pad.Right, 0),
		H: maxf(r.H-pad.Top-pad.Bottom, 0),
	}
	if inner.W <= 0 || inner.H <= 0 {
		return
	}
	cols := int(term.Columns.Get())
	rows := int(term.Lines.Get())
	if cols <= 0 {
		cols = int(inner.W / cellW)
	}
	if rows <= 0 {
		rows = int(inner.H / cellH)
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	term.Fit(cols, rows)
	view := term.View(rows)
	textClip := intersect(clip, inner)

	for row, line := range view.Lines {
		y := inner.Y + float64(row)*cellH
		if y >= inner.Bottom() {
			break
		}
		for col := 0; col < len(line); {
			cell := line[col]
			if cell.Hidden {
				col++
				continue
			}
			end := col + 1
			for end < len(line) && sameTermCell(line[end], cell) {
				end++
			}
			run := make([]rune, 0, end-col)
			for i := col; i < end; i++ {
				r := line[i].R
				if r == 0 {
					r = ' '
				}
				run = append(run, r)
			}
			cellFG, cellBG := termColors(cell, fg, bg)
			x := inner.X + float64(col)*cellW
			w := float64(end-col) * cellW
			if cellBG != bg {
				l.fillRect(tree.Rect{X: x, Y: y, W: w, H: cellH}, cellBG, 0, 0, 0, textClip)
			}
			if !cell.Hidden {
				l.termRun(string(run), cell, cellFG, x, y, w, cellH, st, textClip)
			}
			col = end
		}
	}

	if view.CursorOn && view.CursorRow >= 0 && view.CursorRow < len(view.Lines) {
		row, col := view.CursorRow, view.CursorCol
		if col < 0 || col >= view.Cols {
			col = view.Cols - 1
		}
		if row >= 0 && row < view.Rows {
			x := inner.X + float64(col)*cellW
			y := inner.Y + float64(row)*cellH
			l.fillRect(tree.Rect{X: x, Y: y, W: cellW, H: cellH}, fg, 0, 0, 0, textClip)
			cell := tree.TerminalCell{R: ' '}
			if row < len(view.Lines) && col < len(view.Lines[row]) {
				cell = view.Lines[row][col]
			}
			if cell.R != 0 && cell.R != ' ' {
				l.textIn(string(cell.R), st, bg, tree.Rect{X: x, Y: y, W: cellW, H: cellH}, AlignStart, textClip)
			}
		}
	}
}

func sameTermCell(a, b tree.TerminalCell) bool {
	return a.FG == b.FG && a.BG == b.BG && a.Bold == b.Bold && a.Faint == b.Faint &&
		a.Italic == b.Italic && a.Underline == b.Underline && a.Reverse == b.Reverse &&
		a.Strike == b.Strike && a.Hidden == b.Hidden
}

func termColors(cell tree.TerminalCell, fg, bg uint32) (uint32, uint32) {
	if cell.FG != 0 {
		fg = cell.FG
	}
	if cell.BG != 0 {
		bg = cell.BG
	}
	if cell.Reverse {
		fg, bg = bg, fg
	}
	if cell.Faint {
		fg = withAlpha(fg, 0.6)
	}
	return fg, bg
}

func (l *List) termRun(s string, cell tree.TerminalCell, color uint32, x, y, w, h float64, st m3.TextStyle, clip tree.Rect) {
	if cell.FG != 0 {
		color = cell.FG
	}
	if cell.Reverse {
		color = cell.BG
		if color == 0 {
			color = l.Theme.Colors.SurfaceContainerLowest
		}
	}
	if cell.Faint {
		color = withAlpha(color, 0.6)
	}
	if cell.Bold {
		st.Weight = int(text.WeightBold)
	}
	l.textIn(s, st, color, tree.Rect{X: x, Y: y, W: w, H: h}, AlignStart, clip)
	if cell.Underline {
		l.fillRect(tree.Rect{X: x, Y: y + h - 1, W: w, H: 1}, color, 0, 0, 0, clip)
	}
	if cell.Strike {
		l.fillRect(tree.Rect{X: x, Y: y + h/2, W: w, H: 1}, color, 0, 0, 0, clip)
	}
}
