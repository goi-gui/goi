package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/platform"
)

const tableUI = `Window {
	Column {
		Table {
			id: table
			width: 400
			selectable: single
			TableColumn { key: "name" label: "Name" width: 200 sortable: true }
			TableColumn { key: "role" label: "Role" sortable: true }
			TableColumn { key: "age" label: "Age" width: 80 align: end numeric: true sortable: true }

			TableRow { TableCell { value: "Kaan" Text { text: "Kaan" } } TableCell { value: "Engineer" Text { text: "Engineer" } } TableCell { value: "31" Text { text: "31" } } }
			TableRow { TableCell { value: "Ada" Text { text: "Ada" } } TableCell { value: "Mathematician" Text { text: "Mathematician" } } TableCell { value: "36" Text { text: "36" } } }
			TableRow { TableCell { value: "Linus" Text { text: "Linus" } } TableCell { value: "Kernel" Text { text: "Kernel" } } TableCell { value: "54" Text { text: "54" } } }
		}
	}
}`

func tableCenter(t *testing.T, w *Window, id string, col int) (float64, float64) {
	t.Helper()
	tbl, err := ByID[*Table](w, id)
	if err != nil {
		t.Fatal(err)
	}
	cols := tbl.TableColumns()
	if col < 0 || col >= len(cols) {
		t.Fatalf("no column %d", col)
	}
	r := absoluteRect(cols[col])
	return r.X + r.W/2, r.Y + r.H/2
}

// TestTableHeaderClickSorts covers the whole header gesture: the first click
// sorts ascending, the second reverses, and the event carries the column key.
func TestTableHeaderClickSorts(t *testing.T) {
	_, win, in := inputFixture(t, tableUI, 500, 600)
	tbl := MustByID[*Table](win, "table")
	var got []SortOrder
	tbl.OnSort(func(o SortOrder) { got = append(got, o) })

	x, y := tableCenter(t, win, "table", 2) // Age
	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if len(got) != 1 {
		t.Fatalf("sort events = %v, want one", got)
	}
	if got[0].Key != "age" || !got[0].Ascending || got[0].Column != 2 {
		t.Fatalf("first sort = %+v", got[0])
	}
	if first := tbl.CellValue(0, 0); first != "Kaan" {
		t.Fatalf("ascending first row = %q, want Kaan (31)", first)
	}

	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	if len(got) != 2 || got[1].Ascending {
		t.Fatalf("second click = %+v, want descending", got)
	}
	if first := tbl.CellValue(0, 0); first != "Linus" {
		t.Fatalf("descending first row = %q, want Linus (54)", first)
	}
}

// TestTableHeaderSortsText checks a text column: scores compare as words unless
// the column says numeric.
func TestTableHeaderSortsText(t *testing.T) {
	_, win, in := inputFixture(t, tableUI, 500, 600)
	tbl := MustByID[*Table](win, "table")
	x, y := tableCenter(t, win, "table", 0) // Name
	in.MouseDown(x, y, 0, false)
	in.MouseUp(x, y, 0)
	var names []string
	for i := 0; i < tbl.RowCount(); i++ {
		names = append(names, tbl.CellValue(i, 0))
	}
	want := []string{"Ada", "Kaan", "Linus"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}

// TestTableRowClickSelects checks selection: the cell press selects its row,
// the event reports the display index, and pressing another row moves it.
func TestTableRowClickSelects(t *testing.T) {
	_, win, in := inputFixture(t, tableUI, 500, 600)
	tbl := MustByID[*Table](win, "table")
	var picks []int
	tbl.OnSelect(func(i int) { picks = append(picks, i) })

	rows := tbl.TableRows()
	r := absoluteRect(rows[1])
	in.MouseDown(r.X+10, r.Y+r.H/2, 0, false)
	in.MouseUp(r.X+10, r.Y+r.H/2, 0)
	if len(picks) != 1 || picks[0] != 1 {
		t.Fatalf("selection events = %v, want row 1", picks)
	}
	if tbl.SelectedIndex() != 1 {
		t.Fatalf("SelectedIndex = %d, want 1", tbl.SelectedIndex())
	}
	if !rows[1].Selected.Get() || rows[0].Selected.Get() {
		t.Fatalf("row flags did not follow the selection")
	}
}

// TestTableSortKeepsSelectionMeaning checks the index a click reports after
// sorting: it is the row the reader sees, not the declaration position.
func TestTableSortKeepsSelectionMeaning(t *testing.T) {
	_, win, in := inputFixture(t, tableUI, 500, 600)
	tbl := MustByID[*Table](win, "table")
	tbl.SortBy("age", false) // Linus 54, Ada 36, Kaan 31
	rows := tbl.TableRows()
	r := absoluteRect(rows[0])
	var picked int
	tbl.OnSelect(func(i int) { picked = i })
	in.MouseDown(r.X+10, r.Y+r.H/2, 0, false)
	in.MouseUp(r.X+10, r.Y+r.H/2, 0)
	if picked != 0 || tbl.CellValue(0, 0) != "Linus" {
		t.Fatalf("selected %d (%q), want the first sorted row", picked, tbl.CellValue(0, 0))
	}
}

// TestTablePointerShapes checks the affordance: a sortable header and a cell
// show a hand, and an unsortable header does not.
func TestTablePointerShapes(t *testing.T) {
	_, win, in := inputFixture(t, tableUI, 500, 600)
	var got platform.Cursor
	in.setCursor = func(c platform.Cursor) { got = c }

	x, y := tableCenter(t, win, "table", 0)
	in.MouseMove(x, y)
	if got != platform.CursorPointer {
		t.Fatalf("sortable header cursor = %v, want pointer", got)
	}

	tbl := MustByID[*Table](win, "table")
	row := tbl.TableRows()[0]
	r := absoluteRect(row)
	// A cell selects its row, so it reads as clickable too.
	got = -1
	in.MouseMove(r.X+10, r.Y+r.H/2)
	if got != platform.CursorPointer {
		t.Fatalf("cell cursor = %v, want the hand", got)
	}

	// The compact table's columns are not sortable.
	_, win2, in2 := inputFixture(t, `Window { Column {
		Table {
			id: plain
			width: 200
			TableColumn { label: "A" width: 100 }
			TableRow { TableCell { value: "x" Text { text: "x" } } }
		}
	} }`, 400, 300)
	col := MustByID[*Table](win2, "plain").TableColumns()[0]
	cr := absoluteRect(col)
	got2 := platform.Cursor(-1)
	in2.setCursor = func(c platform.Cursor) { got2 = c }
	in2.MouseMove(cr.X+cr.W/2, cr.Y+cr.H/2)
	if got2 == platform.CursorPointer {
		t.Fatal("an unsortable header showed the hand")
	}
}

// TestTableGoSideAPI covers the Go path: SortBy by key, SetCell and a row count.
func TestTableGoSideAPI(t *testing.T) {
	_, win, _ := inputFixture(t, tableUI, 500, 600)
	tbl := MustByID[*Table](win, "table")
	if tbl.RowCount() != 3 {
		t.Fatalf("RowCount = %d", tbl.RowCount())
	}
	if !tbl.SortBy("name", false) {
		t.Fatal("SortBy(name, false) did not find the column")
	}
	if tbl.CellValue(0, 0) != "Linus" {
		t.Fatalf("first row = %q, want Linus", tbl.CellValue(0, 0))
	}
	if tbl.SortBy("nope", true) {
		t.Fatal("SortBy found a column that does not exist")
	}
	if !tbl.SetCellValue(1, 0, "Grace") || tbl.CellValue(1, 0) != "Grace" {
		t.Fatalf("SetCell = %q", tbl.CellValue(1, 0))
	}
	if tbl.SetCellValue(9, 0, "x") {
		t.Fatal("SetCell accepted an out-of-range row")
	}
	tbl.SetSelectedRow(2)
	if tbl.SelectedIndex() != 2 || !tbl.TableRows()[2].Selected.Get() {
		t.Fatal("SetSelectedRow did not carry to the row")
	}
	tbl.SetSelectedRow(-1)
	if tbl.SelectedIndex() != -1 {
		t.Fatal("SetSelectedRow(-1) did not clear")
	}
}

// TestTableRowsSortedForLayoutAndHit keeps the two walks honest: the row a
// paint test reads first is the row a click at the top selects.
func TestTableRowsSortedForLayoutAndHit(t *testing.T) {
	_, win, in := inputFixture(t, tableUI, 500, 600)
	tbl := MustByID[*Table](win, "table")
	if !tbl.SortBy("name", true) {
		t.Fatal("SortBy(name) did not find the column")
	}
	rows := tbl.TableRows()
	top := absoluteRect(rows[0])
	hit := in.hit(top.X+5, top.Y+5)
	if hit == nil {
		t.Fatal("nothing hit at the first row")
	}
	if hit.Kind() != "TableCell" {
		t.Fatalf("hit = %s, want the first cell", hit.Kind())
	}
}

const tableUIMulti = `Window { Column { Table {
	id: table
	width: 400
	selectable: multiple
	TableColumn { key: "name" label: "Name" width: 200 sortable: true }
	TableColumn { key: "role" label: "Role" sortable: true }
	TableColumn { key: "age" label: "Age" width: 80 align: end numeric: true sortable: true }
	TableRow { TableCell { value: "Kaan" Text { text: "Kaan" } } TableCell { value: "Engineer" Text { text: "Engineer" } } TableCell { value: "31" Text { text: "31" } } }
	TableRow { TableCell { value: "Ada" Text { text: "Ada" } } TableCell { value: "Mathematician" Text { text: "Mathematician" } } TableCell { value: "36" Text { text: "36" } } }
	TableRow { TableCell { value: "Linus" Text { text: "Linus" } } TableCell { value: "Kernel" Text { text: "Kernel" } } TableCell { value: "54" Text { text: "54" } } }
} } }`

// pressRow clicks the middle of a row.
func pressRow(t *testing.T, in *interactor, tbl *Table, i int) {
	t.Helper()
	r := absoluteRect(tbl.TableRows()[i])
	in.MouseDown(r.X+10, r.Y+r.H/2, 0, false)
	in.MouseUp(r.X+10, r.Y+r.H/2, 0)
}

// TestTableSingleSelectionTogglesOff checks the single mode: the selected row
// deselects on a second click, and only one row is ever selected.
func TestTableSingleSelectionTogglesOff(t *testing.T) {
	_, win, in := inputFixture(t, tableUI, 500, 600)
	tbl := MustByID[*Table](win, "table")

	pressRow(t, in, tbl, 1)
	if got := tbl.SelectedRows(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("selected = %v, want [1]", got)
	}
	pressRow(t, in, tbl, 1)
	if got := tbl.SelectedRows(); len(got) != 0 {
		t.Fatalf("selected = %v, want none after a second click", got)
	}
	pressRow(t, in, tbl, 0)
	pressRow(t, in, tbl, 2)
	if got := tbl.SelectedRows(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("selected = %v, want only the last row", got)
	}
}

// TestTableMultipleSelectionToggles checks the multiple mode: each click adds
// the row, a second click removes it, and the event reports every toggle.
func TestTableMultipleSelectionToggles(t *testing.T) {
	_, win, in := inputFixture(t, tableUIMulti, 500, 600)
	tbl := MustByID[*Table](win, "table")
	var picks []int
	tbl.OnSelect(func(i int) { picks = append(picks, i) })

	pressRow(t, in, tbl, 0)
	pressRow(t, in, tbl, 1)
	if got := tbl.SelectedRows(); !equalInts(got, []int{0, 1}) {
		t.Fatalf("selected = %v, want [0 1]", got)
	}
	pressRow(t, in, tbl, 1)
	if got := tbl.SelectedRows(); !equalInts(got, []int{0}) {
		t.Fatalf("selected = %v, want [0] after re-clicking row 1", got)
	}
	pressRow(t, in, tbl, 0)
	if got := tbl.SelectedRows(); len(got) != 0 {
		t.Fatalf("selected = %v, want none", got)
	}
	if !equalInts(picks, []int{0, 1, 1, 0}) {
		t.Fatalf("events = %v", picks)
	}
}

// TestTableDefaultSelectableNone checks that an unqualified table is inert: no
// press event, no selection and no pointer affordance.
func TestTableDefaultSelectableNone(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 300
		TableColumn { label: "A" width: 150 }
		TableColumn { label: "B" width: 150 }
		TableRow { TableCell { value: "a" Text { text: "a" } } TableCell { value: "b" Text { text: "b" } } }
	} } }`
	_, win, in := inputFixture(t, src, 500, 400)
	tbl := MustByID[*Table](win, "table")
	picked := false
	tbl.OnSelect(func(int) { picked = true })
	pressRow(t, in, tbl, 0)
	if picked || len(tbl.SelectedRows()) != 0 {
		t.Fatalf("a default table selected a row (event=%v, rows=%v)", picked, tbl.SelectedRows())
	}

	var got platform.Cursor
	in.setCursor = func(c platform.Cursor) { got = c }
	cell := absoluteRect(tbl.TableRows()[0])
	in.MouseMove(cell.X+10, cell.Y+cell.H/2)
	if got == platform.CursorPointer {
		t.Fatal("a cell of a default table showed the hand")
	}
}

// TestTableMultipleGoSide covers SetSelectedRows and ToggleRow from Go.
func TestTableMultipleGoSide(t *testing.T) {
	_, win, _ := inputFixture(t, tableUIMulti, 500, 600)
	tbl := MustByID[*Table](win, "table")
	tbl.SetSelectedRows(0, 2)
	if got := tbl.SelectedRows(); !equalInts(got, []int{0, 2}) {
		t.Fatalf("selected = %v, want [0 2]", got)
	}
	if tbl.SelectedIndex() != 0 {
		t.Fatalf("SelectedIndex = %d, want the first selected row", tbl.SelectedIndex())
	}
	tbl.ToggleRow(0)
	if got := tbl.SelectedRows(); !equalInts(got, []int{2}) {
		t.Fatalf("selected = %v, want [2]", got)
	}
	tbl.ToggleRow(2)
	if got := tbl.SelectedRows(); len(got) != 0 {
		t.Fatalf("selected = %v, want none", got)
	}
	// Single mode keeps one row even when Go passes several.
	tbl.SetSelectedRow(-1)
	_, win2, _ := inputFixture(t, tableUI, 500, 600)
	single := MustByID[*Table](win2, "table")
	single.SetSelectedRows(0, 1, 2)
	if got := single.SelectedRows(); !equalInts(got, []int{2}) {
		t.Fatalf("single selected = %v, want [2]", got)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTableClickOnCellContentSelectsTheRow keeps the container change honest: a
// press lands on the Text inside a cell, and the row still selects.
func TestTableClickOnCellContentSelectsTheRow(t *testing.T) {
	_, win, in := inputFixture(t, tableUI, 500, 600)
	tbl := MustByID[*Table](win, "table")
	cell := tbl.TableRows()[1]
	text := cell.Kids()[0].Node()
	r := absoluteRect(text)
	in.MouseDown(r.X+2, r.Y+r.H/2, 0, false)
	in.MouseUp(r.X+2, r.Y+r.H/2, 0)
	if got := tbl.SelectedRows(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("selected = %v, want the row whose text was clicked", got)
	}
}

// TestTableButtonInCellKeepsItsClick checks the escape hatch: an interactive
// child inside a selectable cell acts, and does not also select the row.
func TestTableButtonInCellKeepsItsClick(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 400
		selectable: single
		TableColumn { label: "A" width: 400 }
		TableRow {
			TableCell {
				Button { id: act label: "Act" }
			}
		}
	} } }`
	_, win, in := inputFixture(t, src, 500, 400)
	tbl := MustByID[*Table](win, "table")
	btn := MustByID[*Button](win, "act")
	clicked := false
	btn.OnClick(func(ClickEvent) { clicked = true })
	r := absoluteRect(btn)
	in.MouseDown(r.X+r.W/2, r.Y+r.H/2, 0, false)
	in.MouseUp(r.X+r.W/2, r.Y+r.H/2, 0)
	if !clicked {
		t.Fatal("a button inside a cell did not receive its click")
	}
	if got := tbl.SelectedRows(); len(got) != 0 {
		t.Fatalf("selected = %v, want the button to win", got)
	}
}
