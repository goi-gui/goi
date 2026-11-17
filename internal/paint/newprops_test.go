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
	l := m3list(t, `Window { Dialog { filledIcon: true Button { label: "Go" leadingIcon: add } } }`)
	if op := icon(l, "add"); op == nil || op.FilledIcon {
		t.Fatalf("the card's setting leaked into the button: %+v", op)
	}
}

// TestFilledIconSlots: filledIcon fills both glyphs of a two-slot component,
// and the per-slot properties fill one without touching the other.
func TestFilledIconSlots(t *testing.T) {
	icons := func(l *List) map[string]bool {
		out := map[string]bool{}
		for i := range l.Ops {
			if l.Ops[i].Kind == Text && l.Ops[i].Icon {
				out[l.Ops[i].Text] = l.Ops[i].FilledIcon
			}
		}
		return out
	}
	fields := func(props string) map[string]bool {
		return icons(m3list(t, `Window { TextField {
			label: "Name" text: "Ada" leadingIcon: person trailingIcon: clear `+props+`
		} }`))
	}

	if got := fields(`filledLeadingIcon: true`); !got["person"] || got["clear"] {
		t.Fatalf("filledLeadingIcon only: %v", got)
	}
	if got := fields(`filledTrailingIcon: true`); got["person"] || !got["clear"] {
		t.Fatalf("filledTrailingIcon only: %v", got)
	}
	both := fields(`filledLeadingIcon: true filledTrailingIcon: true`)
	if !both["person"] || !both["clear"] {
		t.Fatalf("both slots: %v", both)
	}
	if got := fields(`filledIcon: true`); !got["person"] || !got["clear"] {
		t.Fatalf("filledIcon fills both: %v", got)
	}
	none := fields(``)
	if none["person"] || none["clear"] {
		t.Fatalf("defaults are filled: %v", none)
	}

	// A leading-only component takes the slot property too.
	got := icons(m3list(t, `Window { Button { label: "Go" leadingIcon: add filledLeadingIcon: true } }`))
	if !got["add"] {
		t.Fatalf("filledLeadingIcon on a button: %v", got)
	}
}

// TestSelectedNavigationItemFillsItsIcon: selection is a state signal, so the
// active destination draws the filled glyph even when the design left the
// filled properties off; an idle one stays outlined.
func TestSelectedNavigationItemFillsItsIcon(t *testing.T) {
	icon := func(l *List, name string) *Op {
		for i := range l.Ops {
			if l.Ops[i].Kind == Text && l.Ops[i].Icon && l.Ops[i].Text == name {
				return &l.Ops[i]
			}
		}
		return nil
	}
	l := m3list(t, `Window { NavigationRail {
		height: 200
		NavigationItem { label: "On" leadingIcon: home selected: true }
		NavigationItem { label: "Off" leadingIcon: search }
	} }`)
	if op := icon(l, "home"); op == nil || !op.FilledIcon {
		t.Fatalf("the selected destination is not filled: %+v", op)
	}
	if op := icon(l, "search"); op == nil || op.FilledIcon {
		t.Fatalf("an idle destination is filled: %+v", op)
	}

	// The idle item can still be filled by the property.
	l = m3list(t, `Window { NavigationRail {
		height: 200
		NavigationItem { label: "Off" leadingIcon: search filledLeadingIcon: true }
	} }`)
	if op := icon(l, "search"); op == nil || !op.FilledIcon {
		t.Fatalf("filledLeadingIcon stopped working: %+v", op)
	}
}

// TestRippleProperty: `ripple: true` gives a plain element the press feedback a
// button has, `ripple: false` takes it away from a button, and the disc is cut
// to the element's own shape so it cannot spill outside it.
func TestRippleProperty(t *testing.T) {
	press := func(tr *tree.Tree, id string) {
		n, _ := tr.ByID(id)
		b := n.Base()
		b.RippleOn, b.Ripple = true, 0.5
		b.RippleX, b.RippleY = b.Bounds().X+b.Bounds().W/2, b.Bounds().Y+b.Bounds().H/2
	}
	discs := func(l *List) []Op {
		var out []Op
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Shape == Circle {
				out = append(out, op)
			}
		}
		return out
	}
	build := func(src string, id string) (*List, tree.Rect) {
		t.Helper()
		tr, diag := tree.BuildSource([]byte(src), nil)
		if !diag.Empty() {
			t.Fatalf("%v", diag)
		}
		layout.New(nil).Layout(tr.Root, 400, 400)
		press(tr, id)
		l := &List{}
		l.Build(tr, 400, 400)
		n, _ := tr.ByID(id)
		return l, n.Base().Bounds()
	}

	l, box := build(`Window { Column {
		Rectangle { id: box width: 120 height: 40 radius: 12 ripple: true }
	} }`, "box")
	got := discs(l)
	if len(got) != 1 {
		t.Fatalf("%d ripple discs, want one:\n%s", len(got), l.Dump())
	}
	op := got[0]
	if op.Clip != rectArr(box) {
		t.Fatalf("ripple clip = %v, want the element's box %v", op.Clip, box)
	}
	if float64(op.ClipRadius) != 12 {
		t.Fatalf("ripple radius = %v, want the box's 12", op.ClipRadius)
	}
	if op.ClipBox != rectArr(box) {
		t.Fatalf("ripple clip box = %v, want the element's own box", op.ClipBox)
	}

	l, _ = build(`Window { Column {
		Rectangle { id: box width: 120 height: 40 }
	} }`, "box")
	if got := discs(l); len(got) != 0 {
		t.Fatalf("an element without ripple drew %d discs", len(got))
	}

	// A button ripples by default; false turns it off.
	buildButton := func(props string) *List {
		tr, diag := tree.BuildSource([]byte(`Window { Button { id: b label: "Go" `+props+` } }`), nil)
		if !diag.Empty() {
			t.Fatalf("%v", diag)
		}
		layout.New(nil).Layout(tr.Root, 400, 400)
		n, _ := tr.ByID("b")
		c := n.(*tree.Button).M3()
		c.RippleOn, c.Ripple = true, 0.5
		c.RippleX, c.RippleY = n.Base().Bounds().W/2, n.Base().Bounds().H/2
		l := &List{}
		l.Build(tr, 400, 400)
		return l
	}
	if got := discs(buildButton("")); len(got) != 1 {
		t.Fatalf("a pressed button drew %d discs", len(got))
	}
	if got := discs(buildButton("ripple: false")); len(got) != 0 {
		t.Fatalf("ripple: false still drew %d discs", len(got))
	}
}

// TestRippleDrawsOverAnImage: an image paints its bitmap after the box, so the
// generic ripple has to come last or the picture covers it.
func TestRippleDrawsOverAnImage(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Image { id: pic src: "https://example.com/a.png" width: 120 height: 80 radius: 8 ripple: true }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, _ := tr.ByID("pic")
	im := n.(*tree.Image)
	im.Data = &img.Data{W: 4, H: 4, Pix: make([]byte, 4*4*4)}
	layout.New(nil).Layout(tr.Root, 400, 400)
	nb := n.Base()
	box := nb.Bounds()
	nb.RippleOn, nb.Ripple = true, 0.2
	nb.RippleX, nb.RippleY = box.X+box.W/2, box.Y+box.H/2
	l := &List{}
	l.Build(tr, 400, 400)

	bitmap, disc := -1, -1
	for i, op := range l.Ops {
		if op.Kind == Bitmap {
			bitmap = i
		}
		if op.Kind == Fill && op.Shape == Circle {
			disc = i
		}
	}
	if bitmap < 0 || disc < 0 {
		t.Fatalf("bitmap=%d disc=%d\n%s", bitmap, disc, l.Dump())
	}
	if disc < bitmap {
		t.Fatal("the ripple is under the image")
	}
	if got := l.Ops[disc]; float64(got.ClipRadius) != 8 {
		t.Fatalf("ripple radius = %v, want the image's 8", got.ClipRadius)
	}
}

// TestTextPaddingShiftsTheGlyphs: paint draws inside the padding, not at the
// box edge.
func TestTextPaddingShiftsTheGlyphs(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column { crossAxisAlign: start
		Text { text: "hello" padding: 10 }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 500, 500)
	l := &List{}
	l.Build(tr, 500, 500)
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "hello" {
			if op.TextX != 10 {
				t.Fatalf("glyph x = %v, want the 10dp padding", op.TextX)
			}
			return
		}
	}
	t.Fatalf("no glyph:\n%s", l.Dump())
}

// TestColumnClipsOnlyWhenItOverflows: a column is a scroll container by
// default, but it only cuts its children once they are taller than the frame;
// the shadow of a button in a fitting column stays outside the box.
func TestColumnClipsOnlyWhenItOverflows(t *testing.T) {
	clipOf := func(src string) [4]float32 {
		tr, diag := tree.BuildSource([]byte(src), nil)
		if !diag.Empty() {
			t.Fatalf("%v", diag)
		}
		layout.New(nil).Layout(tr.Root, 300, 200)
		l := &List{}
		l.Build(tr, 300, 200)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Rect[3] == 600 {
				return op.Clip
			}
		}
		return [4]float32{}
	}
	if got := clipOf(`Window { Column { width: 100 height: 100 Rectangle { width: 100 height: 600 background: #ff0000 } } }`); got != [4]float32{0, 0, 100, 100} {
		t.Fatalf("overflowing column clip = %v, want its frame", got)
	}
	if got := clipOf(`Window { Column { scroll: false width: 100 height: 100 Rectangle { width: 100 height: 600 background: #ff0000 } } }`); got != [4]float32{0, 0, 300, 200} {
		t.Fatalf("plain column clip = %v, want the window", got)
	}
}

// TestDisabledDimsCardMenuAndTab: a disabled Card, FAB menu and Tab look
// disabled, not only refuse the press.
func TestDisabledDimsCardMenuAndTab(t *testing.T) {
	alpha := func(c uint32) uint32 { return c & 0xFF }
	dimmed := func(l *List, kind Kind, text string) bool {
		for _, op := range l.Ops {
			if op.Kind == kind && (text == "" || op.Text == text) && alpha(op.Color) < 0xFF {
				return true
			}
		}
		return false
	}

	if !dimmed(m3list(t, `Window { Card { label: "x" disabled: true } }`), Text, "x") {
		t.Fatal("a disabled card did not dim its label")
	}
	if !dimmed(m3list(t, `Window { FABMenu { icon: add disabled: true } }`), Fill, "") {
		t.Fatal("a disabled FAB menu did not dim its container")
	}
	if !dimmed(m3list(t, `Window { Tabs { Tab { label: "a" disabled: true } } }`), Text, "a") {
		t.Fatal("a disabled tab did not dim its label")
	}
}

// TestTabSelectedDrivesTheIndicator: `selected: true` in a design marks the
// active tab, underline included, so the design and a click agree.
func TestTabSelectedDrivesTheIndicator(t *testing.T) {
	l := m3list(t, `Window { Tabs { Tab { label: "a" } Tab { label: "b" selected: true } } }`)
	var under []float32
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Rect[3] == 3 {
			under = append(under, op.Rect[0])
		}
	}
	// Two tabs in a 400dp window: the second starts at 200, and its underline
	// sits 8dp in.
	if len(under) != 1 || under[0] != 208 {
		t.Fatalf("indicator at %v, want one under the second tab", under)
	}
}

// TestBorderAxesDrawOnlyTheirSides: borderX draws the left and right bands in
// their own colour, and leaves the top and bottom alone.
func TestBorderAxesDrawOnlyTheirSides(t *testing.T) {
	l := m3list(t, `Window { Column {
		Rectangle {
			id:     box
			width:  100
			height: 60
			borderXThickness: 2
			borderXColor:     #ff0000
		}
	} }`)
	var bands []Op
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == 0xFF0000FF && op.Rect[3] > 10 {
			bands = append(bands, op)
		}
	}
	if len(bands) != 2 {
		t.Fatalf("%d red bands, want the two vertical sides:\n%s", len(bands), l.Dump())
	}
	for _, op := range bands {
		if op.Rect[2] != 2 {
			t.Fatalf("band width = %v, want 2", op.Rect[2])
		}
		if op.Rect[0] == 0 && op.Rect[2] != 2 {
			t.Fatalf("unexpected band %+v", op)
		}
	}

	// The horizontal axis colours only the top and bottom.
	l = m3list(t, `Window { Column {
		Rectangle { width: 100 height: 60 borderYThickness: 3 borderYColor: #00ff00 }
	} }`)
	var horizontal int
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == 0x00FF00FF && op.Rect[2] > 10 && op.Rect[3] == 3 {
			horizontal++
		}
	}
	if horizontal != 2 {
		t.Fatalf("%d horizontal bands, want the two horizontal sides:\n%s", horizontal, l.Dump())
	}
}
