package paint

import (
	"fmt"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

const tableSrc = `Window { Column {
	Table {
		id: table
		width: 420
		%s
		TableColumn { key: "name" label: "Name" width: 200 sortable: true }
		TableColumn { key: "role" label: "Role" %s }
		TableColumn { key: "age" label: "Age" width: 80 align: end numeric: true sortable: true }

		TableRow {
			TableCell { value: "Kaan" Text { text: "Kaan" } }
			TableCell { value: "Engineer" Text { text: "Engineer" } }
			TableCell { value: "31" Text { text: "31" } }
		}
		TableRow {
			TableCell { value: "Ada" Text { text: "Ada" } }
			TableCell { value: "Mathematician" Text { text: "Mathematician" } }
			TableCell { value: "36" Text { text: "36" } }
		}
	}
} }`

func tableSource(tableProps, roleProps string) string {
	return fmt.Sprintf(tableSrc, tableProps, roleProps)
}

// tableTree builds and lays out the fixture, so a test can read bounds as well
// as ops.
func tableTree(t *testing.T, tableProps string) *tree.Tree {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(tableSource(tableProps, "")), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 500, 400)
	return tr
}

// TestTablePaintsHeaderAndCells covers the basics: the header labels, the cell
// text, and one op per cell.
func TestTablePaintsHeaderAndCells(t *testing.T) {
	l := list(t, tableSource("", ""), 500, 400)
	texts := map[string]int{}
	for _, op := range l.Ops {
		if op.Kind == Text {
			texts[op.Text]++
		}
	}
	for _, want := range []string{"Name", "Role", "Age", "Kaan", "Engineer", "31", "Ada", "Mathematician", "36"} {
		if texts[want] == 0 {
			t.Fatalf("missing %q in:\n%s", want, l.Dump())
		}
	}
}

// TestTableStripesAndSelection checks the chrome the table paints itself: the
// selected band uses the scheme's secondary container, and stripes alternate.
func TestTableStripesAndSelection(t *testing.T) {
	col := m3.Light().Colors
	l := list(t, tableSource("striped: true", ""), 500, 400)
	stripes, selected := 0, 0
	for _, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		switch op.Color {
		case col.SecondaryContainer:
			selected++
		case withAlpha(col.OnSurface, 0.03):
			stripes++
		}
	}
	if stripes != 1 {
		t.Fatalf("striped bands = %d, want one for the second of two rows", stripes)
	}
	if selected != 0 {
		t.Fatalf("a row is selected before anyone selects it")
	}

	l = list(t, tableSource("striped: true selectable: single selectedRow: 1", ""), 500, 400)
	selected = 0
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == col.SecondaryContainer {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("selected bands = %d, want exactly the selected row", selected)
	}
}

// TestTableSortArrow checks the header feedback: only the sorted column shows an
// arrow, and the arrow follows the direction.
func TestTableSortArrow(t *testing.T) {
	arrow := func(prop string) string {
		l := list(t, tableSource(prop, ""), 500, 400)
		for _, op := range l.Ops {
			if op.Kind == Text && op.Icon && (op.Text == "arrow_upward" || op.Text == "arrow_downward") {
				return op.Text
			}
		}
		return ""
	}
	if got := arrow(""); got != "" {
		t.Fatalf("unsorted table drew %q", got)
	}
	if got := arrow("sortColumn: 0"); got != "arrow_upward" {
		t.Fatalf("ascending arrow = %q", got)
	}
	if got := arrow("sortColumn: 0 sortAscending: false"); got != "arrow_downward" {
		t.Fatalf("descending arrow = %q", got)
	}
}

// TestTableHidesHeader checks showHeader: false: no labels, and the rows start
// where the header would have been.
func TestTableHidesHeader(t *testing.T) {
	l := list(t, tableSource("showHeader: false", ""), 500, 400)
	for _, op := range l.Ops {
		if op.Kind == Text && (op.Text == "Name" || op.Text == "Age") {
			t.Fatalf("header label %q drawn although the header is hidden", op.Text)
		}
	}
	tr := tableTree(t, "showHeader: false")
	n, _ := tr.ByID("table")
	row := n.(*tree.Table).TableRows()[0]
	if y := row.Base().Bounds().Y; y != 0 {
		t.Fatalf("first row y = %v, want the table top", y)
	}
}

// TestTableCellsShareTheWidth checks the column arithmetic: fixed columns keep
// their width, the flexible one takes what is left, and the cells tile the row.
func TestTableCellsShareTheWidth(t *testing.T) {
	tr := tableTree(t, "")
	n, _ := tr.ByID("table")
	tbl := n.(*tree.Table)
	cols := tbl.TableColumns()
	want := []float64{200, 140, 80}
	for i, c := range cols {
		if got := c.Base().Bounds().W; got != want[i] {
			t.Fatalf("column %d width = %v, want %v", i, got, want[i])
		}
	}
	row := tbl.TableRows()[0]
	cells := tree.CellNodes(row)
	if len(cells) != 3 {
		t.Fatalf("cells = %d", len(cells))
	}
	if cells[0].Base().Bounds().X != 0 || cells[2].Base().Bounds().X != 340 {
		t.Fatalf("cell xs = %v, %v", cells[0].Base().Bounds().X, cells[2].Base().Bounds().X)
	}
	if cells[2].Base().Bounds().W != 80 {
		t.Fatalf("last cell width = %v", cells[2].Base().Bounds().W)
	}
}

// TestTablePercentWidths checks `width: 25%`: a percentage column takes that
// share of the table's content box, and the flexible column takes the rest.
func TestTablePercentWidths(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 500
		TableColumn { key: "a" width: 10% }
		TableColumn { key: "b" width: 100 }
		TableColumn { key: "c" }
		TableRow { TableCell { value: "a" Text { text: "a" } } TableCell { value: "b" Text { text: "b" } } TableCell { value: "c" Text { text: "c" } } }
	} } }`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 800, 600)
	n, _ := tr.ByID("table")
	cols := n.(*tree.Table).TableColumns()
	want := []float64{50, 100, 350}
	for i, c := range cols {
		if got := c.Base().Bounds().W; got != want[i] {
			t.Fatalf("column %d width = %v, want %v", i, got, want[i])
		}
	}
}

// TestTableHeaderAlignsWithCells keeps a column's label over its values: the
// header and the cells start at the same inset, and an end-aligned column ends
// at the same edge.
func TestTableHeaderAlignsWithCells(t *testing.T) {
	l := list(t, tableSource("", ""), 500, 400)
	box := func(text string) Op {
		t.Helper()
		for _, op := range l.Ops {
			if op.Kind == Text && !op.Icon && op.Text == text {
				return op
			}
		}
		t.Fatalf("no text op %q", text)
		return Op{}
	}
	if h, c := box("Name"), box("Kaan"); h.TextX != c.TextX {
		t.Fatalf("start-aligned header x = %v, cell x = %v", h.TextX, c.TextX)
	}
	if h, c := box("Age"), box("31"); h.TextX+h.BoxW != c.TextX+c.BoxW {
		t.Fatalf("end-aligned header right = %v, cell right = %v", h.TextX+h.BoxW, c.TextX+c.BoxW)
	}
}

// TestTableColumnDividers checks that a table with a surface separates its
// columns with a vertical rule, and a plain table does not.
func TestTableColumnDividers(t *testing.T) {
	dividers := func(variant string) int {
		l := list(t, tableSource("variant: "+variant, ""), 500, 400)
		n := 0
		for _, op := range l.Ops {
			if op.Kind == Fill && op.BorderWidth == 0 && op.Rect[2] == 1 && op.Rect[3] > 1 {
				n++
			}
		}
		return n
	}
	// Three columns have two boundaries.
	for _, v := range []string{"outlined", "filled"} {
		if got := dividers(v); got != 2 {
			t.Fatalf("%s dividers = %d, want 2", v, got)
		}
	}
	if got := dividers("plain"); got != 0 {
		t.Fatalf("plain dividers = %d, want none", got)
	}
}

// TestTableVariantsPaintDistinctChrome pins the three styles: outlined draws a
// surface and a border, filled draws the tinted surface, plain draws neither.
// A variant that fell through to the same ops would be a no-op in a design.
func TestTableVariantsPaintDistinctChrome(t *testing.T) {
	col := m3.Light().Colors
	chrome := func(variant string) (border bool, tinted bool, surface bool) {
		l := list(t, tableSource("variant: "+variant, ""), 500, 400)
		for _, op := range l.Ops {
			if op.Kind != Fill {
				continue
			}
			if op.BorderWidth > 0 {
				border = true
			}
			switch op.Color {
			case col.SurfaceContainerLow:
				tinted = true
			case col.Surface:
				surface = true
			}
		}
		return
	}
	if b, tinted, surface := chrome("outlined"); !b || !surface || tinted {
		t.Fatalf("outlined: border=%v surface=%v tinted=%v", b, surface, tinted)
	}
	if b, tinted, _ := chrome("filled"); !b || !tinted {
		t.Fatalf("filled: border=%v tinted=%v", b, tinted)
	}
	if b, tinted, surface := chrome("plain"); b || tinted || surface {
		t.Fatalf("plain: border=%v surface=%v tinted=%v", b, surface, tinted)
	}
}

// TestTableRowBackground paints a row's own background: it is a common
// property, and the row draws it over the table's bands.
func TestTableRowBackground(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 300
		striped: true
		TableColumn { label: "A" width: 150 }
		TableColumn { label: "B" width: 150 }
		TableRow { background: #ff0000ff TableCell { value: "a1" Text { text: "a1" } } TableCell { value: "b1" Text { text: "b1" } } }
		TableRow { TableCell { value: "a2" Text { text: "a2" } } TableCell { value: "b2" Text { text: "b2" } } }
	} } }`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 500, 400)
	l := &List{}
	l.Build(tr, 500, 400)

	tbl, _ := tr.ByID("table")
	tab := tbl.(*tree.Table)
	rows := tab.TableRows()
	b := rows[0].Base().Bounds()
	base := tab.Base().Bounds()
	want := tree.Rect{X: base.X + b.X, Y: base.Y + b.Y, W: b.W, H: b.H}

	const want0 = uint32(0xff0000ff)
	for _, op := range l.Ops {
		if op.Kind != Fill || op.Color != want0 {
			continue
		}
		got := tree.Rect{X: float64(op.Rect[0]), Y: float64(op.Rect[1]), W: float64(op.Rect[2]), H: float64(op.Rect[3])}
		if got == want {
			return
		}
	}
	t.Fatalf("no row background at the first row's box %v:\n%s", want, l.Dump())
}

// TestTableMultipleSelectedRowsPaint checks that multiple mode paints every
// selected row, whether the design seeded the flags or Go set them.
func TestTableMultipleSelectedRowsPaint(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 300
		selectable: multiple
		TableColumn { label: "A" width: 150 }
		TableColumn { label: "B" width: 150 }
		TableRow { selected: true TableCell { value: "a1" Text { text: "a1" } } TableCell { value: "b1" Text { text: "b1" } } }
		TableRow { TableCell { value: "a2" Text { text: "a2" } } TableCell { value: "b2" Text { text: "b2" } } }
		TableRow { selected: true TableCell { value: "a3" Text { text: "a3" } } TableCell { value: "b3" Text { text: "b3" } } }
	} } }`
	l := list(t, src, 500, 400)
	col := m3.Light().Colors
	bands := 0
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == col.SecondaryContainer {
			bands++
		}
	}
	if bands != 2 {
		t.Fatalf("selected bands = %d, want the first and third row", bands)
	}
}

// TestTableColumnIcon checks that a header can carry an icon: the glyph sits
// before the label, and a column with only an icon still paints.
func TestTableColumnIcon(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 300
		TableColumn { label: "Name" icon: "person" width: 200 }
		TableColumn { icon: "cake" width: 100 }
		TableRow { TableCell { value: "Kaan" Text { text: "Kaan" } } TableCell { value: "31" Text { text: "31" } } }
	} } }`
	l := list(t, src, 500, 400)
	x := map[string]float32{}
	for _, op := range l.Ops {
		if op.Kind == Text && op.Icon {
			x[op.Text] = op.Rect[0]
		}
	}
	if _, ok := x["person"]; !ok {
		t.Fatalf("missing the column icon:\n%s", l.Dump())
	}
	if _, ok := x["cake"]; !ok {
		t.Fatalf("an icon-only column drew no icon:\n%s", l.Dump())
	}
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "Name" {
			if x["person"] >= op.Rect[0] {
				t.Fatalf("icon x = %v, label x = %v; want the icon first", x["person"], op.Rect[0])
			}
			return
		}
	}
	t.Fatalf("missing the header label:\n%s", l.Dump())
}

// TestTableCellDrawsItsChildren keeps the new cell contract: the cell's value is
// only the sort key, so what reaches the screen is the child component.
func TestTableCellDrawsItsChildren(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 400
		TableColumn { label: "Task" width: 200 }
		TableColumn { label: "Status" width: 200 }
		TableRow {
			TableCell {
				value: "2026-11-08"
				Text { text: "8 Nov 2026" }
			}
			TableCell {
				value: "done"
				Row {
					spacing: 6
					Chip { label: "shipped" variant: input selected: true }
				}
			}
		}
	} } }`
	l := list(t, src, 500, 400)
	texts := map[string]bool{}
	for _, op := range l.Ops {
		if op.Kind == Text {
			texts[op.Text] = true
		}
	}
	if !texts["8 Nov 2026"] {
		t.Fatalf("the cell's Text child was not drawn:\n%s", l.Dump())
	}
	if !texts["shipped"] {
		t.Fatalf("a Chip inside a cell was not drawn:\n%s", l.Dump())
	}
	if texts["2026-11-08"] || texts["done"] {
		t.Fatalf("a cell value leaked onto the screen:\n%s", l.Dump())
	}
}

// TestTableSortUsesValue checks that ordering reads `value`, not the drawn text,
// so a formatted date sorts by its ISO key.
func TestTableSortUsesValue(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 400
		TableColumn { key: "when" label: "When" sortable: true }
		TableRow { TableCell { value: "2026-02-01" Text { text: "1 Feb" } } }
		TableRow { TableCell { value: "2025-12-24" Text { text: "24 Dec" } } }
		TableRow { TableCell { value: "2026-01-15" Text { text: "15 Jan" } } }
	} } }`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 500, 400)
	n, _ := tr.ByID("table")
	tbl := n.(*tree.Table)
	if !tbl.SortBy("when", true) {
		t.Fatal("SortBy did not find the column")
	}
	want := []string{"2025-12-24", "2026-01-15", "2026-02-01"}
	for i, v := range want {
		if got := tbl.CellValue(i, 0); got != v {
			t.Fatalf("row %d = %q, want %q", i, got, v)
		}
	}
}

// TestTableCellAlignsItsContent puts an end-aligned column's child against the
// trailing inset, and a start-aligned one against the leading inset.
func TestTableCellAlignsItsContent(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 400
		TableColumn { label: "A" width: 200 align: end }
		TableColumn { label: "B" width: 200 align: start }
		TableRow {
			TableCell { Text { text: "right" } }
			TableCell { Text { text: "left" } }
		}
	} } }`
	l := list(t, src, 500, 400)
	box := func(text string) [4]float32 {
		for _, op := range l.Ops {
			if op.Kind == Text && op.Text == text {
				return op.Rect
			}
		}
		t.Fatalf("%q was not drawn:\n%s", text, l.Dump())
		return [4]float32{}
	}
	right, left := box("right"), box("left")
	// Column A spans 0..200 with a 16dp inset, so an end-aligned label ends at
	// 184; column B starts at 200, so a start-aligned label begins at 216.
	if end := right[0] + right[2]; end < 181 || end > 187 {
		t.Fatalf("end-aligned text ends at %v, want about 184", end)
	}
	if left[0] < 213 || left[0] > 219 {
		t.Fatalf("start-aligned text starts at %v, want about 216", left[0])
	}
}

// TestTableCellIconLeadsContent checks that a cell icon stays left of the
// children and layout leaves room for it.
func TestTableCellIconLeadsContent(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 400
		TableColumn { label: "Event" width: 400 }
		TableRow {
			TableCell { icon: check_circle Text { text: "Build passed" } }
		}
	} } }`
	l := list(t, src, 500, 400)
	var icon, label [4]float32
	for _, op := range l.Ops {
		switch {
		case op.Kind == Text && op.Icon && op.Text == "check_circle":
			icon = op.Rect
		case op.Kind == Text && op.Text == "Build passed":
			label = op.Rect
		}
	}
	if icon[2] == 0 || label[2] == 0 {
		t.Fatalf("icon or label missing:\n%s", l.Dump())
	}
	if icon[0]+icon[2] > label[0] {
		t.Fatalf("icon %v overlaps the label %v", icon, label)
	}
}

// TestTableRowDisabledDimsContent checks the row switch now that a cell holds
// children: the child's colour is faded, not just a cell text colour.
func TestTableRowDisabledDimsContent(t *testing.T) {
	const src = `Window { Column { Table {
		id: table
		width: 300
		TableColumn { label: "A" width: 300 }
		TableRow { disabled: true TableCell { value: "on" Text { text: "on" } } }
		TableRow { TableCell { value: "off" Text { text: "off" } } }
	} } }`
	l := list(t, src, 500, 400)
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "on" {
			if op.Color == m3.Light().Colors.OnSurface {
				t.Fatalf("a disabled row's text drew at full strength")
			}
			return
		}
	}
	t.Fatalf("the disabled row's text was not drawn:\n%s", l.Dump())
}
