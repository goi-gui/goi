package layout

import (
	"github.com/goi-gui/goi/internal/tree"
)

// table column metrics, in dp.
const (
	tableCellInset = 16.0
	// tableFlexColumn is the intrinsic width one flexible column asks for when
	// the table has no declared width.
	tableFlexColumn = 140.0
)

// tableWidths resolves each column's width inside total. A percentage takes
// its share of total, a declared width is kept as written, and the rest share
// what is left, never below TableColumnMin. The remainder goes to the last
// flexible column so the row always fills the table.
func tableWidths(cols []*tree.TableColumn, total float64) []float64 {
	out := make([]float64, len(cols))
	fixed := 0.0
	flex := 0
	for i, c := range cols {
		if f, ok := c.PercentWidth(); ok {
			out[i] = total * f
			fixed += out[i]
			continue
		}
		if w := c.Width(); w >= 0 {
			out[i] = w
			fixed += w
			continue
		}
		flex++
	}
	if flex == 0 {
		return out
	}
	share := (total - fixed) / float64(flex)
	if share < tree.TableColumnMin {
		share = tree.TableColumnMin
	}
	last := -1
	for i := range out {
		if out[i] == 0 {
			out[i] = share
			last = i
		}
	}
	if last >= 0 {
		out[last] = total - fixed - share*float64(flex-1)
		if out[last] < tree.TableColumnMin {
			out[last] = tree.TableColumnMin
		}
	}
	return out
}

// tableHeaderHeight is the header band, or 0 when the design hides it.
func tableHeaderHeight(t *tree.Table) float64 {
	if !t.ShowHeader.Get() {
		return 0
	}
	if h := t.HeaderHeight.Get(); h > 0 {
		return h
	}
	return tree.DefaultHeaderHeight
}

// rowHeight is a row's height: its own declaration, else the table's setting.
func rowHeight(row *tree.TableRow, def float64) float64 {
	if h := row.Height(); h >= 0 {
		return h
	}
	if def > 0 {
		return def
	}
	return tree.DefaultRowHeight
}

// tableNatural is the intrinsic size: the declared column widths plus a request
// per flexible column, and the header plus every row.
func (e *Engine) tableNatural(b *tree.NodeBase, availW, availH float64) (float64, float64) {
	t, ok := b.Node().(*tree.Table)
	if !ok {
		return e.containerNatural(b, availW, availH)
	}
	cols := t.TableColumns()
	rows := t.TableRows()
	fixed, flex := 0.0, 0
	for _, c := range cols {
		// A percentage needs a table width to be a percentage of; while the
		// width is still unknown, ask for the flexible default like any auto
		// column.
		if _, ok := c.PercentWidth(); ok {
			flex++
			continue
		}
		if w := c.Width(); w >= 0 {
			fixed += w
		} else {
			flex++
		}
	}
	w := fixed + float64(flex)*tableFlexColumn
	if b.Width() >= 0 {
		w = b.Width()
	}
	h := tableHeaderHeight(t)
	def := t.RowHeight.Get()
	for _, r := range rows {
		h += rowHeight(r, def)
	}
	return w, h
}

// tableChildren places the header columns and every row, sharing the widths
// across the table's content box.
func (e *Engine) tableChildren(b *tree.NodeBase, w, h float64) {
	t, ok := b.Node().(*tree.Table)
	if !ok {
		return
	}
	pe, bw := b.PaddingEdges(), b.BorderWidth()
	left := pe.Left + bw
	top := pe.Top + bw
	cw := maxf(w-pe.Left-pe.Right-2*bw, 0)

	cols := t.TableColumns()
	widths := tableWidths(cols, cw)
	hh := tableHeaderHeight(t)

	x := left
	for i, c := range cols {
		if hh <= 0 {
			c.SetBounds(tree.Rect{})
			continue
		}
		c.SetBounds(tree.Rect{X: x, Y: top, W: widths[i], H: hh})
		x += widths[i]
	}

	def := t.RowHeight.Get()
	y := top + hh
	for _, row := range t.TableRows() {
		rh := rowHeight(row, def)
		row.SetBounds(tree.Rect{X: left, Y: y, W: cw, H: rh})
		// Cells are placed inside the row, so their bounds are row-relative,
		// which is what every other child in the tree uses.
		e.tableCells(row, 0, 0, rh, widths)
		y += rh
	}
}

// tableCells spreads one row's cells across the columns. A row with more cells
// than columns puts the extras past the last column's width, which is a design
// mistake the validator can still catch by looking at the file.
func (e *Engine) tableCells(row *tree.TableRow, x, y, h float64, widths []float64) {
	for i, cell := range tree.CellNodes(row) {
		w := 0.0
		if i < len(widths) {
			w = widths[i]
		}
		cell.SetBounds(tree.Rect{X: x, Y: y, W: w, H: h})
		x += w
	}
}
