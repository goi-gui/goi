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
			TableCell { text: "Kaan" }
			TableCell { text: "Engineer" }
			TableCell { text: "31" }
		}
		TableRow {
			TableCell { text: "Ada" }
			TableCell { text: "Mathematician" }
			TableCell { text: "36" }
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

	l = list(t, tableSource("striped: true selectedRow: 1", ""), 500, 400)
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
		TableRow { TableCell { text: "a" } TableCell { text: "b" } TableCell { text: "c" } }
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
