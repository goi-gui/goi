package tree

import (
	"sort"
	"strconv"
	"strings"
)

// Table defaults, in dp, following the M3 data-table metrics.
const (
	DefaultRowHeight    = 48.0
	DefaultHeaderHeight = 40.0
	// TableColumnMin is the width a flexible column never drops below.
	TableColumnMin = 80.0
)

// TableColumns returns the TableColumn children, in declaration order.
func (t *Table) TableColumns() []*TableColumn {
	var out []*TableColumn
	for _, k := range t.Kids() {
		if c, ok := k.Node().(*TableColumn); ok {
			out = append(out, c)
		}
	}
	return out
}

// TableRows returns the row children in display order: the sort column decides
// when one is set, ties keep declaration order. Layout, paint and hit-testing
// all use this, so the row a reader sees is the row a click selects.
func (t *Table) TableRows() []*TableRow {
	rows := make([]*TableRow, 0, len(t.Kids()))
	for _, k := range t.Kids() {
		c, ok := k.Node().(*TableRow)
		if !ok || !c.Visible() {
			continue
		}
		rows = append(rows, c)
	}
	col := t.SortIndex()
	if col < 0 || len(rows) < 2 {
		return rows
	}
	numeric := false
	if cols := t.TableColumns(); col < len(cols) {
		numeric = cols[col].Numeric.Get()
	}
	asc := t.SortAscending.Get()
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := CellText(rows[i], col), CellText(rows[j], col)
		if numeric {
			af, aok := numberIn(a)
			bf, bok := numberIn(b)
			if aok && bok {
				if asc {
					return af < bf
				}
				return af > bf
			}
		}
		if asc {
			return strings.ToLower(a) < strings.ToLower(b)
		}
		return strings.ToLower(a) > strings.ToLower(b)
	})
	return rows
}

// SortIndex returns the column being sorted, or -1.
func (t *Table) SortIndex() int {
	i := int(t.SortColumn.Get())
	if i < 0 || i >= len(t.TableColumns()) {
		return -1
	}
	return i
}

// SortState reports the sorted column index (-1 for none) and the direction.
func (t *Table) SortState() (int, bool) {
	return t.SortIndex(), t.SortAscending.Get()
}

// Sort sorts by column index. It only reorders the rows, so no layout of the
// surrounding page changes beyond the row positions.
func (t *Table) Sort(column int, ascending bool) {
	if column < 0 || column >= len(t.TableColumns()) {
		t.SortColumn.Set(-1)
	} else {
		t.SortColumn.Set(float64(column))
	}
	t.SortAscending.Set(ascending)
	t.MarkDirty(DirtyLayout)
}

// SortBy sorts by a column key, which is what Go usually knows. It reports
// false when no column declares that key.
func (t *Table) SortBy(key string, ascending bool) bool {
	for i, c := range t.TableColumns() {
		if c.Key.Get() == key {
			t.Sort(i, ascending)
			return true
		}
	}
	return false
}

// Rows returns the rows in display order, as the shared component type.
func (t *Table) Rows() []*Component {
	rows := t.TableRows()
	out := make([]*Component, len(rows))
	for i, r := range rows {
		out[i] = &r.Component
	}
	return out
}

// RowCount returns how many rows the table shows.
func (t *Table) RowCount() int { return len(t.TableRows()) }

// Selection mode names, as `selectable` accepts them.
const (
	SelectNone     = "none"
	SelectSingle   = "single"
	SelectMultiple = "multiple"
)

// SelectableMode returns the table's selection mode, defaulting to single.
func (t *Table) SelectableMode() string {
	switch t.Selectable.Get() {
	case SelectNone, SelectMultiple:
		return t.Selectable.Get()
	}
	return SelectSingle
}

// SelectedIndex returns the first selected row in display order, or -1.
func (t *Table) SelectedIndex() int {
	sel := t.SelectedRows()
	if len(sel) == 0 {
		return -1
	}
	return sel[0]
}

// SelectedRows returns every selected row's display index, in display order.
// In single mode the `selectedRow` cell is the state; in multiple mode the
// rows' own `selected` flags are, so a design can seed them row by row.
func (t *Table) SelectedRows() []int {
	rows := t.TableRows()
	switch t.SelectableMode() {
	case SelectNone:
		return nil
	case SelectMultiple:
		out := make([]int, 0, len(rows))
		for i, r := range rows {
			if r.Selected.Get() {
				out = append(out, i)
			}
		}
		return out
	}
	i := int(t.SelectedRow.Get())
	if i < 0 || i >= len(rows) {
		return nil
	}
	return []int{i}
}

// SetSelectedRow selects a row by display index; -1 clears the selection.
func (t *Table) SetSelectedRow(i int) {
	if i < 0 || i >= len(t.TableRows()) {
		i = -1
	}
	t.SelectedRow.Set(float64(i))
	for n, row := range t.TableRows() {
		row.Selected.Set(n == i)
	}
	t.MarkDirty(DirtyPaint)
}

// SetSelectedRows replaces the selection with the given display indices.
// Multiple keeps them all, single keeps the last one and none clears them.
func (t *Table) SetSelectedRows(indexes ...int) {
	rows := t.TableRows()
	want := make(map[int]bool, len(indexes))
	for _, i := range indexes {
		if i >= 0 && i < len(rows) {
			want[i] = true
		}
	}
	if t.SelectableMode() != SelectMultiple && len(want) > 1 {
		last := -1
		for _, i := range indexes {
			if want[i] {
				last = i
			}
		}
		clear(want)
		if last >= 0 {
			want[last] = true
		}
	}
	if t.SelectableMode() == SelectNone {
		clear(want)
	}
	t.SelectedRow.Set(-1)
	for i, row := range rows {
		row.Selected.Set(want[i])
		if want[i] {
			t.SelectedRow.Set(float64(i))
		}
	}
	t.MarkDirty(DirtyPaint)
}

// ToggleRow flips one row the way a click does, following the selectable mode:
// single holds at most one row, multiple toggles each independently, none
// ignores the press. It reports false for an out-of-range index.
func (t *Table) ToggleRow(i int) bool {
	rows := t.TableRows()
	if i < 0 || i >= len(rows) {
		return false
	}
	switch t.SelectableMode() {
	case SelectNone:
		return false
	case SelectMultiple:
		row := rows[i]
		row.Selected.Set(!row.Selected.Get())
		if row.Selected.Get() {
			t.SelectedRow.Set(float64(i))
		} else if !t.anyRowSelected() {
			t.SelectedRow.Set(-1)
		}
		t.MarkDirty(DirtyPaint)
		return true
	}
	if t.SelectedIndex() == i {
		t.SetSelectedRow(-1)
	} else {
		t.SetSelectedRow(i)
	}
	return true
}

// anyRowSelected reports whether any row carries the selection flag.
func (t *Table) anyRowSelected() bool {
	for _, row := range t.TableRows() {
		if row.Selected.Get() {
			return true
		}
	}
	return false
}

// SetCell sets a cell's text, addressing it by row display index and column
// index. It reports false when either is out of range.
func (t *Table) SetCell(row, column int, text string) bool {
	rows := t.TableRows()
	if row < 0 || row >= len(rows) {
		return false
	}
	cells := CellNodes(rows[row])
	if column < 0 || column >= len(cells) {
		return false
	}
	cells[column].Text.Set(text)
	return true
}

// CellText returns a cell's text by display row and column index, "" when
// either is out of range.
func (t *Table) CellText(row, column int) string {
	rows := t.TableRows()
	if row < 0 || row >= len(rows) {
		return ""
	}
	return CellText(rows[row], column)
}

// CellNodes returns one row's cells in column order.
func CellNodes(row *TableRow) []*TableCell {
	var out []*TableCell
	for _, k := range row.Kids() {
		if c, ok := k.Node().(*TableCell); ok {
			out = append(out, c)
		}
	}
	return out
}

// CellText returns the text of a row's cell at a column index, "" when the row
// has no such cell.
func CellText(row *TableRow, column int) string {
	cells := CellNodes(row)
	if column < 0 || column >= len(cells) {
		return ""
	}
	return cells[column].Text.Get()
}

// numberIn reads the first number in s, so "12 items" sorts like 12.
func numberIn(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	fields := strings.Fields(s)
	for _, f := range fields {
		trimmed := strings.Trim(f, "$€₺£¥,()%")
		if v, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}
