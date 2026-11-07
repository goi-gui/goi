package paint

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// tableCellInset is the horizontal padding inside a cell, following the M3 data
// table's 16dp edge.
const tableCellInset = 16.0

// tableHeaderHeight mirrors the layout's rule: the declared height, or zero when
// the design hides the header.
func tableHeaderHeight(t *tree.Table) float64 {
	if !t.ShowHeader.Get() {
		return 0
	}
	if h := t.HeaderHeight.Get(); h > 0 {
		return h
	}
	return tree.DefaultHeaderHeight
}

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

// columnFor returns the column that owns a cell, by the cell's position in its
// row.
func columnFor(cell *tree.TableCell) (*tree.Table, *tree.TableColumn, int) {
	p := cell.Base().Parent()
	if p == nil {
		return nil, nil, -1
	}
	row, ok := p.(*tree.TableRow)
	if !ok {
		return nil, nil, -1
	}
	gp := row.Base().Parent()
	if gp == nil {
		return nil, nil, -1
	}
	t, ok := gp.(*tree.Table)
	if !ok {
		return nil, nil, -1
	}
	for i, c := range tree.CellNodes(row) {
		if c == cell {
			cols := t.TableColumns()
			if i < len(cols) {
				return t, cols[i], i
			}
			return t, nil, i
		}
	}
	return t, nil, -1
}

// cellAlign resolves the alignment a cell uses: its own, else the column's,
// else end for a numeric column and start otherwise.
func cellAlign(cell *tree.TableCell) Align {
	if a := cell.Align.Get(); a != "" {
		return alignOf(a)
	}
	_, col, _ := columnFor(cell)
	if col == nil {
		return AlignStart
	}
	if a := col.Align.Get(); a != "" {
		return alignOf(a)
	}
	if col.Numeric.Get() {
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
func (l *List) paintTable(t *tree.Table, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	rad := t.Base().Radius()
	variant := variant(&t.Component, "outlined")

	// The bands follow the table's rounded frame, so a rounded table does not
	// show square corners behind its border.
	box := r
	band := func(b tree.Rect, color uint32) {
		if color == 0 {
			return
		}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect, Rect: rectArr(b), Color: color,
			Clip: clipArr(clip), ClipRadius: float32(rad), ClipBox: rectArr(box),
		})
	}

	switch variant {
	case "plain":
		// The design already put the table on its own surface.
	case "filled":
		band(r, col.SurfaceContainerLow)
	default: // outlined
		band(r, col.Surface)
	}

	hh := tableHeaderHeight(t)
	if hh > 0 {
		band(tree.Rect{X: r.X, Y: r.Y, W: r.W, H: hh}, withAlpha(col.OnSurface, 0.04))
		l.hLine(r.X, r.Y+hh, r.W, col.OutlineVariant, clip)
	}

	rows := t.TableRows()
	def := t.RowHeight.Get()
	y := r.Y + hh
	selected := t.SelectedIndex()
	for i, row := range rows {
		rh := row.Height()
		if rh < 0 {
			rh = def
			if rh <= 0 {
				rh = tree.DefaultRowHeight
			}
		}
		if i == selected {
			band(tree.Rect{X: r.X, Y: y, W: r.W, H: rh}, col.SecondaryContainer)
		} else if t.Striped.Get() && i%2 == 1 {
			band(tree.Rect{X: r.X, Y: y, W: r.W, H: rh}, withAlpha(col.OnSurface, 0.03))
		}
		if i > 0 {
			l.hLine(r.X, y, r.W, withAlpha(col.OutlineVariant, 0.6), clip)
		}
		y += rh
	}

	if variant != "plain" {
		// A border last, so the separators cannot paint over it.
		l.fillRect(r, 0, rad, col.OutlineVariant, 1, clip)
	}
}

// paintTableColumn draws one header: its label and, when the column is the
// sorted one, the direction arrow. A sortable column reads as tappable through
// the pointer shape the input layer sets.
func (l *List) paintTableColumn(c *tree.Component, r tree.Rect, clip tree.Rect) {
	tc, ok := c.Node().(*tree.TableColumn)
	if !ok || tc.Label.Get() == "" {
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
	box := r
	if sorted {
		// Room for the arrow, on the side the text ends at.
		arrowGap := 24.0
		if align == AlignEnd {
			box.X += arrowGap
			box.W -= arrowGap
		} else {
			box.W -= arrowGap
		}
	}
	l.textIn(tc.Label.Get(), l.style("labelLarge"), fg, box, align, clip)
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

// paintTableCell draws one value, aligned by the cell or its column.
func (l *List) paintTableCell(c *tree.Component, r tree.Rect, clip tree.Rect) {
	cell, ok := c.Node().(*tree.TableCell)
	if !ok {
		return
	}
	col := l.Theme.Colors
	fg := col.OnSurface
	if cell.Disabled.Get() {
		fg = withAlpha(fg, m3.DisabledOpacity)
	}
	box := alignedBox(r)
	if icon := cell.Icon.Get(); icon != "" && cell.Text.Get() == "" {
		align := cellAlign(cell)
		cx := r.X + tableCellInset + 9
		switch align {
		case AlignCenter:
			cx = r.X + r.W/2
		case AlignEnd:
			cx = r.X + r.W - tableCellInset - 9
		}
		l.iconIn(icon, 18, fg, cx, r.Y+r.H/2, clip)
		return
	}
	l.textIn(cell.Text.Get(), l.style("bodyMedium"), fg, box, cellAlign(cell), clip)
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
