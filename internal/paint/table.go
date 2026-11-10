package paint

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// tableCellInset is the horizontal padding inside a cell, following the M3 data
// table's 16dp edge.
const tableCellInset = 16.0

// alignOf maps a design alignment name to the painter's.
func alignOf(name string) Align {
	switch name {
	case "center":
		return AlignCenter
	case "end":
		return AlignEnd
	}
	return AlignStart
}

// alignedBox narrows a cell box by the cell inset and returns it with the
// alignment the painter should use.
func alignedBox(r tree.Rect) tree.Rect {
	inset := tableCellInset
	if r.W < inset*2 {
		inset = r.W / 4
	}
	return tree.Rect{X: r.X + inset, Y: r.Y, W: r.W - inset*2, H: r.H}
}

// paintTable draws the surface, the header band, the row stripes, the selected
// row and the separators. Cells and headers paint their own content on top.
//
// Bands and rules are part of the scrollable content: they move with the rows by
// the table's offsets and are clipped to its viewport, so a table with a fixed
// height does not leak stripes past its frame. A sticky header keeps the band
// and the column labels at the top while the rows pass underneath.
func (l *List) paintTable(t *tree.Table, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	rad := t.Base().Radius()
	variant := variant(&t.Component, "outlined")
	view := t.Base().ScrollViewport(r)
	tableClip := intersect(clip, view)

	// The bands follow the table's rounded frame, so a rounded table does not
	// show square corners behind its border.
	box := r
	band := func(b tree.Rect, color uint32, c tree.Rect) {
		if color == 0 {
			return
		}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect, Rect: rectArr(b), Color: color,
			Clip: clipArr(c), ClipRadius: float32(rad), ClipBox: rectArr(box),
		})
	}

	switch variant {
	case "plain":
		// The design already put the table on its own surface.
	case "filled":
		band(r, col.SurfaceContainerLow, tableClip)
	default: // outlined
		band(r, col.Surface, tableClip)
	}

	rows := t.TableRows()
	cols := t.TableColumns()
	hh := t.HeaderBand()
	sticky := hh > 0 && t.StickyHeader.Get()
	ox, oy := t.Base().ScrollShift()

	// Layout positions include the table's padding and border, so the header
	// and the first row start where the cells actually are.
	headerY := r.Y + hh
	if len(cols) > 0 {
		headerY = r.Y + cols[0].Base().Bounds().Y
	}
	if !sticky {
		headerY += oy
	}
	rowY := r.Y + hh
	if len(rows) > 0 {
		rowY = r.Y + rows[0].Base().Bounds().Y
	}
	rowY += oy

	rowsClip := tableClip
	if sticky {
		rowsClip = t.RowsClip(r, tableClip)
	}

	if hh > 0 {
		band(tree.Rect{X: r.X, Y: headerY, W: r.W, H: hh}, withAlpha(col.OnSurface, 0.04), tableClip)
		l.hLine(r.X, headerY+hh, r.W, col.OutlineVariant, tableClip)
	}

	// The stripes span the full column width, which may be wider than the frame.
	contentX, contentW := r.X+ox, r.W
	if len(rows) > 0 {
		contentX = r.X + rows[0].Base().Bounds().X + ox
		contentW = rows[0].Base().Bounds().W
	}

	def := t.RowHeight.Get()
	y := rowY
	selected := make(map[int]bool, len(rows))
	for _, i := range t.SelectedRows() {
		selected[i] = true
	}
	for i, row := range rows {
		rh := row.Height()
		if rh < 0 {
			rh = def
			if rh <= 0 {
				rh = tree.DefaultRowHeight
			}
		}
		if selected[i] {
			band(tree.Rect{X: contentX, Y: y, W: contentW, H: rh}, col.SecondaryContainer, rowsClip)
		} else if t.Striped.Get() && i%2 == 1 {
			band(tree.Rect{X: contentX, Y: y, W: contentW, H: rh}, withAlpha(col.OnSurface, 0.03), rowsClip)
		}
		if i > 0 {
			l.hLine(contentX, y, contentW, withAlpha(col.OutlineVariant, 0.6), rowsClip)
		}
		y += rh
	}

	if variant != "plain" {
		l.tableColumnDividers(t, r, ox, oy, headerY, tableClip)
		// A border last, so the separators cannot paint over it.
		l.fillRect(r, 0, rad, col.OutlineVariant, 1, clip)
	}
}

// tableColumnDividers draws a rule between columns, from the header's top to
// the last row's bottom. A plain table has no surface to divide, so it keeps
// its open look. The rule moves with the content and is clipped like the bands.
func (l *List) tableColumnDividers(t *tree.Table, r tree.Rect, ox, oy float64, headerY float64, clip tree.Rect) {
	rows := t.TableRows()
	if len(rows) == 0 {
		return
	}
	first := rows[0].Base().Bounds()
	last := rows[len(rows)-1].Base().Bounds()
	rowBottom := r.Y + last.Y + last.H + oy
	color := withAlpha(l.Theme.Colors.OutlineVariant, 0.6)
	for i, cell := range tree.CellNodes(rows[0]) {
		if i == 0 {
			continue
		}
		x := r.X + first.X + cell.Base().Bounds().X + ox
		l.vLine(x, headerY, rowBottom-headerY, color, clip)
	}
}

// paintTableColumn draws one header: its label and, when the column is the
// sorted one, the direction arrow. A sortable column reads as tappable through
// the pointer shape the input layer sets.
func (l *List) paintTableColumn(c *tree.Component, r tree.Rect, clip tree.Rect) {
	tc, ok := c.Node().(*tree.TableColumn)
	if !ok || (tc.Label.Get() == "" && tc.Icon.Get() == "") {
		return
	}
	col := l.Theme.Colors
	fg := col.OnSurfaceVariant
	sorted := false
	if p := tc.Base().Parent(); p != nil {
		if t, ok := p.(*tree.Table); ok {
			if i := t.SortIndex(); i >= 0 {
				if cols := t.TableColumns(); i < len(cols) && cols[i] == tc {
					sorted = true
					fg = col.OnSurface
				}
			}
		}
	}
	align := alignOf(tc.Align.Get())
	if tc.Align.Get() == "" && tc.Numeric.Get() {
		align = AlignEnd
	}
	// A header shares the cell's inset, so the label sits in the same column as
	// the values below it.
	box := alignedBox(r)
	if sorted {
		// Room for the arrow on the trailing side. Centring loses half of it
		// on each side so the label stays over the cell text.
		box.W -= 24
		if align == AlignCenter {
			box.X += 12
		}
	}
	if icon := tc.Icon.Get(); icon != "" {
		// Icon then label, as one aligned pair, the way a button draws them.
		st := l.style("labelLarge")
		labelW := textWidth(l, tc.Label.Get(), st)
		total := 18.0
		if tc.Label.Get() != "" {
			total += 8 + labelW
		}
		x := box.X
		switch align {
		case AlignCenter:
			x += (box.W - total) / 2
		case AlignEnd:
			x += box.W - total
		}
		l.iconIn(icon, 18, fg, x+9, r.Y+r.H/2, clip)
		l.textIn(tc.Label.Get(), st, fg,
			tree.Rect{X: x + 26, Y: r.Y, W: labelW, H: r.H}, AlignStart, clip)
	} else {
		l.textIn(tc.Label.Get(), l.style("labelLarge"), fg, box, align, clip)
	}
	if !sorted {
		return
	}
	asc := true
	if p := tc.Base().Parent(); p != nil {
		if t, ok := p.(*tree.Table); ok {
			asc = t.SortAscending.Get()
		}
	}
	icon := "arrow_upward"
	if !asc {
		icon = "arrow_downward"
	}
	// The arrow sits at the trailing edge, after the label's room.
	l.iconIn(icon, 18, fg, r.X+r.W-tableCellInset-6, r.Y+r.H/2, clip)
}

// paintTableCell draws the optional leading icon of a cell. The cell's content
// is its children, which the normal paint walk draws, so the icon stops before
// them; layout reserves the same room.
func (l *List) paintTableCell(c *tree.Component, r tree.Rect, clip tree.Rect) {
	cell, ok := c.Node().(*tree.TableCell)
	if !ok {
		return
	}
	icon := cell.Icon.Get()
	if icon == "" {
		return
	}
	fg := l.Theme.Colors.OnSurface
	if cell.RowDisabled() {
		fg = withAlpha(fg, m3.DisabledOpacity)
	}
	inset := tableCellInset
	if r.W < inset*2 {
		inset = r.W / 4
	}
	l.iconIn(icon, 18, fg, r.X+inset+9, r.Y+r.H/2, clip)
}

// hLine draws a 1dp horizontal rule, clipped like the table.
func (l *List) hLine(x, y, w float64, color uint32, clip tree.Rect) {
	if w <= 0 || color == 0 {
		return
	}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:  [4]float32{float32(x), float32(y), float32(w), 1},
		Color: color, Clip: clipArr(clip),
	})
}

// vLine draws a 1dp vertical rule, clipped like the table.
func (l *List) vLine(x, y, h float64, color uint32, clip tree.Rect) {
	if h <= 0 || color == 0 {
		return
	}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:  [4]float32{float32(x), float32(y), 1, float32(h)},
		Color: color, Clip: clipArr(clip),
	})
}
