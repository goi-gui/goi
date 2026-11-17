package paint

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// TestNestedNavigationItemKeepsItsState is the rail-with-a-column bug: an item
// wrapped in a layout node still shows its indicator and still stacks its icon
// and label the way its rail does.
func TestNestedNavigationItemKeepsItsState(t *testing.T) {
	l := m3list(t, `Window {
		NavigationRail {
			height: 300
			Column {
				NavigationItem { label: "One" leadingIcon: tune selected: true }
			}
		}
	}`)
	var pill bool
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == m3.Light().Colors.SecondaryContainer &&
			op.Rect[2] == 56 && op.Rect[3] == 32 {
			pill = true
		}
	}
	if !pill {
		t.Fatalf("a nested selected item drew no indicator:\n%s", l.Dump())
	}
}

// TestNavigationItemColours covers the five colour properties: the label and the
// icon in each state, and the indicator itself.
func TestNavigationItemColours(t *testing.T) {
	l := m3list(t, `Window {
		NavigationRail {
			height: 300
			NavigationItem {
				label: "One" leadingIcon: tune selected: true
				labelColor: #111111 selectedLabelColor: #222222
				iconColor: #333333 selectedIconColor: #444444
				selectedContainerColor: #555555
			}
		}
	}`)
	var pill, label, icon bool
	for _, op := range l.Ops {
		switch {
		case op.Kind == Fill && op.Color == 0x555555FF:
			pill = true
		case op.Kind == Text && op.Text == "One" && op.Color == 0x222222FF:
			label = true
		case op.Kind == Text && op.Text == "tune" && op.Icon && op.Color == 0x444444FF:
			icon = true
		}
	}
	if !pill || !label || !icon {
		t.Fatalf("indicator=%v label=%v icon=%v\n%s", pill, label, icon, l.Dump())
	}
}

// TestChipColours covers the label and both icons taking a colour of their own.
func TestChipColours(t *testing.T) {
	l := m3list(t, `Window { Chip {
		label: "Tag" leadingIcon: star trailingIcon: close
		labelColor: #112233 leadingIconColor: #445566 trailingIconColor: #778899
	} }`)
	var label, lead, trail bool
	for _, op := range l.Ops {
		if op.Kind != Text {
			continue
		}
		switch {
		case op.Text == "Tag" && op.Color == 0x112233FF:
			label = true
		case op.Text == "star" && op.Icon && op.Color == 0x445566FF:
			lead = true
		case op.Text == "close" && op.Icon && op.Color == 0x778899FF:
			trail = true
		}
	}
	if !label || !lead || !trail {
		t.Fatalf("label=%v leading=%v trailing=%v\n%s", label, lead, trail, l.Dump())
	}
}

// TestChipBorderThickness is the zero-thickness rule: an explicit 0 removes the
// chip's own outline, a thickness with a colour draws it, and a thickness alone
// keeps the outline the component already had instead of eating it.
func TestChipBorderThickness(t *testing.T) {
	own := m3.Light().Colors.OutlineVariant
	border := func(l *List) (color uint32, width float32) {
		for _, op := range l.Ops {
			if op.Kind == Fill && op.BorderThickness > 0 {
				return op.BorderColor, op.BorderThickness
			}
		}
		return 0, 0
	}

	if _, w := border(m3list(t, `Window { Chip { label: "A" borderThickness: 0 } }`)); w != 0 {
		t.Fatalf("borderThickness: 0 still drew %vdp", w)
	}

	color, w := border(m3list(t, `Window { Chip { label: "A" borderThickness: 3 borderColor: #ff0000 } }`))
	if color != 0xFF0000FF || w != 3 {
		t.Fatalf("design border = %08x %vdp, want #ff0000 3dp", color, w)
	}

	color, w = border(m3list(t, `Window { Chip { label: "A" borderThickness: 2 } }`))
	if color != own || w != 1 {
		t.Fatalf("border without a colour = %08x %vdp, want the chip's own %08x 1dp",
			color, w, own)
	}
}

// TestTableSelectionColour checks the selected row's band: a named colour wins,
// and no colour keeps SecondaryContainer.
func TestTableSelectionColour(t *testing.T) {
	band := func(l *List, want uint32) bool {
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Color == want && op.Rect[3] > 10 {
				return true
			}
		}
		return false
	}
	src := `Window { Table {
		selectable: single selectedRow: 0 %s
		TableColumn { label: "Name" }
		TableRow { TableCell { value: "Ada" } }
		TableRow { TableCell { value: "Grace" } }
	} }`
	if !band(m3list(t, fmt.Sprintf(src, "selectionColor: #123456")), 0x123456FF) {
		t.Fatal("selectionColor did not paint the row")
	}
	if !band(m3list(t, fmt.Sprintf(src, "")), m3.Light().Colors.SecondaryContainer) {
		t.Fatal("the default selection band is not SecondaryContainer")
	}
}

// TestTextTruncateAndMaxLines covers both clamps: one line cut with an
// ellipsis, and a wrapped paragraph stopped at the line it was given.
func TestTextTruncateAndMaxLines(t *testing.T) {
	texts := func(l *List) []string {
		var out []string
		for _, op := range l.Ops {
			if op.Kind == Text && !op.Icon {
				out = append(out, op.Text)
			}
		}
		return out
	}

	l := m3list(t, `Window { Text {
		text: "the quick brown fox jumps over the lazy dog"
		width: 120 truncate: true
	} }`)
	lines := texts(l)
	if len(lines) != 1 {
		t.Fatalf("truncate drew %d lines: %q", len(lines), lines)
	}
	if !strings.HasSuffix(lines[0], "…") {
		t.Fatalf("truncated line = %q, want an ellipsis", lines[0])
	}
	if lines[0] == "the quick brown fox jumps over the lazy dog" {
		t.Fatal("truncate drew the whole run")
	}

	l = m3list(t, `Window { Text {
		text: "the quick brown fox jumps over the lazy dog"
		width: 120 wrap: true maxLines: 2
	} }`)
	lines = texts(l)
	if len(lines) != 2 {
		t.Fatalf("maxLines: 2 drew %d lines: %q", len(lines), lines)
	}
	if !strings.HasSuffix(lines[1], "…") {
		t.Fatalf("last clamped line = %q, want an ellipsis", lines[1])
	}
}

// TestBreadcrumbDrawsSeparators covers the component: one separator between each
// pair of crumbs, the glyph and colour the design names, and the gap it sits in.
func TestBreadcrumbDrawsSeparators(t *testing.T) {
	l := m3list(t, `Window { Breadcrumb {
		separator: chevron_right separatorColor: #334455
		Text { text: "Featured" }
		Text { text: "Shirts" }
		Text { text: "Tops" }
	} }`)
	var seps int
	for _, op := range l.Ops {
		if op.Kind == Text && op.Icon && op.Text == "chevron_right" && op.Color == 0x334455FF {
			seps++
		}
	}
	if seps != 2 {
		t.Fatalf("%d separators, want one per gap:\n%s", seps, l.Dump())
	}
}

// TestBorderTypeReachesTheShader checks the style travels on the border op,
// where the renderer's dash pattern reads it.
func TestBorderTypeReachesTheShader(t *testing.T) {
	dash := func(kind string) float32 {
		l := m3list(t, `Window { Rectangle {
			width: 60 height: 40 radius: 8
			borderThickness: 2 borderColor: #ffffff borderType: `+kind+`
		} }`)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.BorderThickness > 0 {
				return op.BorderDash
			}
		}
		return -1
	}
	if got := dash("solid"); got != 0 {
		t.Fatalf("solid = %v", got)
	}
	if got := dash("dashed"); got != 1 {
		t.Fatalf("dashed = %v", got)
	}
	if got := dash("dotted"); got != 2 {
		t.Fatalf("dotted = %v", got)
	}
}

// TestImageBorderAndRoundedCrop: an image draws the design's border over itself,
// and the rounded crop follows the image's own box so a scrolled image keeps
// square corners where the viewport cut it.
func TestImageBorderAndRoundedCrop(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Image {
			id: pic src: "https://example.com/a.png" width: 100 height: 100 radius: 16
			borderThickness: 2 borderColor: #00ff00
		}
	} }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	node, _ := tr.ByID("pic")
	im := node.(*tree.Image)
	im.Data = &img.Data{W: 4, H: 4, Pix: make([]byte, 4*4*4)}
	layout.New(nil).Layout(tr.Root, 400, 400)
	l := &List{}
	l.Build(tr, 400, 400)

	var bitmap *Op
	var border bool
	for i := range l.Ops {
		op := &l.Ops[i]
		if op.Kind == Bitmap {
			bitmap = op
		}
		if op.Kind == Fill && op.BorderColor == 0x00FF00FF && op.BorderThickness == 2 {
			border = true
		}
	}
	if bitmap == nil || !border {
		t.Fatalf("bitmap=%v border=%v\n%s", bitmap != nil, border, l.Dump())
	}
	if bitmap.ClipBox == [4]float32{} {
		t.Fatal("the rounded crop rounded the visible rect instead of the image's box")
	}
}

// TestFilledIconProperty: `filledIcon` reaches every glyph a component paints,
// and only that component's: a nested control keeps its own setting.
func TestFilledIconProperty(t *testing.T) {
	icon := func(l *List, name string) *Op {
		for i := range l.Ops {
			if l.Ops[i].Kind == Text && l.Ops[i].Icon && l.Ops[i].Text == name {
				return &l.Ops[i]
			}
		}
		return nil
	}

	op := icon(m3list(t, `Window { Button { label: "Go" leadingIcon: add filledIcon: true } }`), "add")
	if op == nil || !op.FilledIcon {
		t.Fatalf("a filled button icon = %+v", op)
	}
	op = icon(m3list(t, `Window { Button { label: "Go" leadingIcon: add } }`), "add")
	if op == nil || op.FilledIcon {
		t.Fatalf("the default is not outlined: %+v", op)
	}
	op = icon(m3list(t, `Window { Icon { icon: home filledIcon: true } }`), "home")
	if op == nil || !op.FilledIcon {
		t.Fatalf("a standalone filled icon = %+v", op)
	}

	// A filled container must not fill the control inside it.
	l := m3list(t, `Window { Card { filledIcon: true Button { label: "Go" leadingIcon: add } } }`)
	if op := icon(l, "add"); op == nil || op.FilledIcon {
		t.Fatalf("the card's setting leaked into the button: %+v", op)
	}
}
