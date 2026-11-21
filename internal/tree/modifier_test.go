package tree

import "testing"

func buildModifier(t *testing.T, src string) *Tree {
	t.Helper()
	tr, diag := BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	return tr
}

// TestConditionalPicksFirstTrueArm pins the whole point of the modifier: the
// first matching arm wins, later ones are never consulted, and a pass with no
// match puts the property back to the value it had before the modifier.
func TestConditionalPicksFirstTrueArm(t *testing.T) {
	tr := buildModifier(t, `Window {
		Column {
			Text {
				id:   label
				text: conditional { window.width > 500: "wide" window.width > 300: "narrow" }
			}
		}
	}`)
	n, ok := tr.ByID("label")
	if !ok {
		t.Fatal("no label")
	}
	label := n.(*Text)

	read := func(w float64) string {
		tr.SetViewport(w, 600)
		tr.ApplyExpressions()
		return label.Text.Get()
	}
	if got := read(800); got != "wide" {
		t.Fatalf("at 800 text = %q, want the first arm", got)
	}
	if got := read(400); got != "narrow" {
		t.Fatalf("at 400 text = %q, want the second arm", got)
	}
	// Neither arm matches, so the property returns to its own default.
	if got := read(200); got != "" {
		t.Fatalf("at 200 text = %q, want the empty default", got)
	}
}

// TestResponsiveModifierMatchesMap checks that the keyword form is the same
// registration as the old brace map.
func TestResponsiveModifierMatchesMap(t *testing.T) {
	tr := buildModifier(t, `Window {
		Column {
			id:      box
			padding: responsive { base: 8 md: 16 }
		}
	}`)
	box, _ := tr.ByID("box")
	tr.ApplyBreakpoints(1000)
	if got := box.Base().Padding(); got != 16 {
		t.Fatalf("padding = %v, want 16", got)
	}
	tr.ApplyBreakpoints(300)
	if got := box.Base().Padding(); got != 8 {
		t.Fatalf("padding = %v, want the base 8", got)
	}
}

// TestPaddingAxisShorthands covers paddingX/paddingY in a real build: an axis
// sets both of its edges and a single edge still wins, so a design can set a
// vertical rhythm once and override one side.
func TestPaddingAxisShorthands(t *testing.T) {
	tr := buildResponsive(t, `Window {
		Column {
			id: box
			padding: 4
			paddingX: 16
			paddingY: 2
			paddingLeft: 30
		}
	}`)
	box, _ := tr.ByID("box")
	want := Edges{Top: 2, Right: 16, Bottom: 2, Left: 30}
	if got := box.Base().PaddingEdges(); got != want {
		t.Fatalf("padding edges = %+v, want %+v", got, want)
	}
	// An axis left out follows padding, which is what a design that only wrote
	// paddingX expects on the vertical sides.
	tr = buildResponsive(t, `Window {
		Column {
			id: box
			padding: 6
			paddingX: 20
		}
	}`)
	box, _ = tr.ByID("box")
	if got := box.Base().PaddingEdges(); got != (Edges{Top: 6, Right: 20, Bottom: 6, Left: 20}) {
		t.Fatalf("paddingX alone = %+v", got)
	}
}

// TestConditionalColorsRevert is the shopping case: one selected chip and one
// favourite button take a colour while their condition holds, and return to
// their variant colours when it stops holding.
func TestConditionalColorsRevert(t *testing.T) {
	tr := buildModifier(t, `bridge { on bool }
Window {
	Column {
		Chip {
			id:         chip
			background: conditional { bridge.on: theme.primaryContainer }
		}
		IconButton {
			id:        star
			icon:      favorite
			iconColor: conditional { bridge.on: red }
		}
	}
}`)
	chip := tr.ids["chip"].Base()
	star := tr.ids["star"].(*IconButton)
	resolve := func(string) (uint32, bool) { return 0x224466FF, true }
	tr.SetData("on", true)
	tr.ApplyExpressions()
	tr.ResolveColors(resolve)
	if got := chip.colorRoleFor("background"); got != "theme:primaryContainer" {
		t.Fatalf("selected chip role = %q", got)
	}
	if got := star.IconColor.Get(); got != 0xFF0000FF {
		t.Fatalf("favourite icon colour = %08x", got)
	}

	tr.SetData("on", false)
	tr.ApplyExpressions()
	tr.ResolveColors(resolve)
	if got := chip.colorRoleFor("background"); got != "" {
		t.Fatalf("unselected chip kept role %q", got)
	}
	if got := chip.Background(); got != 0 {
		t.Fatalf("unselected chip background = %08x, want the variant default", got)
	}
	if got := star.IconColor.Get(); got != 0 {
		t.Fatalf("unfavourite icon colour = %08x, want the variant default", got)
	}
}

// TestLoopVariableColor is the shopping swatch: a colour column from the bridge
// is a value, not an enum word, so `background: c` has to become an expression.
func TestLoopVariableColor(t *testing.T) {
	tr := buildModifier(t, `bridge { colors []color }
Window {
	Column {
		for _, c := range bridge.colors {
			Rectangle { background: c width: 10 height: 10 }
		}
	}
}`)
	tr.SetData("colors", []uint32{0x112233FF})
	tr.ApplyExpressions()
	for _, n := range tr.All() {
		if r, ok := n.(*Rectangle); ok {
			if got := r.Base().Background(); got != 0x112233FF {
				t.Fatalf("swatch = %08x, want 112233ff", got)
			}
			return
		}
	}
	t.Fatal("the swatch did not build")
}

// TestLoopContinueBreak checks that continue drops the rest of its iteration
// and break stops later iterations before they build nodes.
func TestLoopContinueBreak(t *testing.T) {
	tr := buildModifier(t, `Window {
	Column {
		for i := range 5 {
			if i == 1 { continue }
			if i == 3 { break }
			Text { text: formatf("%d", i) }
		}
	}
}`)
	tr.ApplyExpressions()
	var texts []string
	for _, n := range tr.All() {
		if tx, ok := n.(*Text); ok {
			texts = append(texts, tx.Text.Get())
		}
	}
	if len(texts) != 2 || texts[0] != "0" || texts[1] != "2" {
		t.Fatalf("texts = %v, want [0 2]", texts)
	}
}

// TestNestedListLoop is the shopping gallery: an outer loop over a list of
// lists, skipped by index with continue. Changing the selected index rebuilds
// the outer loop because its body reads the variable.
func TestNestedListLoop(t *testing.T) {
	tr := buildModifier(t, `bridge { images [][]string selected int }
Window {
	Column {
		for i, list := range bridge.images {
			if i != bridge.selected { continue }
			for _, path := range list {
				Text { text: path }
			}
		}
	}
}`)
	images := [][]string{{"a", "b"}, {"c", "d"}}
	tr.SetData("images", images)
	tr.SetData("selected", 1)
	tr.ApplyExpressions()
	if got := texts(tr); len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("selected 1 = %v, want [c d]", got)
	}
	tr.SetData("selected", 0)
	tr.ApplyExpressions()
	if got := texts(tr); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("selected 0 = %v, want [a b]", got)
	}
}

// TestNestedLoopDoesNotDuplicate is the shopping gallery case: an inner loop
// reads the selected index, so it joins the rebuild. Rebuilding the outer loop
// removes the row that owned the old inner loop; the old registration must not
// refill it and append a second set of images.
func TestNestedLoopDoesNotDuplicate(t *testing.T) {
	tr := buildModifier(t, `bridge { images [][]string selected int }
Window {
	Column {
		for i, list := range bridge.images {
			if i != bridge.selected { continue }
			for j, path := range list {
				Text { text: path height: bridge.selected == j ? 500 : 200 }
			}
		}
	}
}`)
	tr.SetData("images", [][]string{{"a", "b", "c"}, {"d", "e", "f"}})
	tr.SetData("selected", 0)
	tr.ApplyExpressions()
	if got := len(texts(tr)); got != 3 {
		t.Fatalf("selected 0 has %d texts, want 3", got)
	}
	tr.SetData("selected", 1)
	tr.ApplyExpressions()
	got := texts(tr)
	if len(got) != 3 || got[0] != "d" || got[1] != "e" || got[2] != "f" {
		t.Fatalf("selected 1 = %v, want [d e f]", got)
	}
}

// TestNestedEmptyLoopDoesNotFillLater is the edge the node check cannot see:
// the old inner loop produced no nodes because its condition filtered them all.
// Its row is still gone, so moving to the next outer iteration must not fill
// that old loop alongside the new one.
func TestNestedEmptyLoopDoesNotFillLater(t *testing.T) {
	tr := buildModifier(t, `bridge { groups [][]string items []string selected int }
Window {
	Column {
		for i, group := range bridge.groups {
			if i != bridge.selected { continue }
			for _, item := range bridge.items {
				if bridge.selected == 0 { continue }
				Text { text: item }
			}
		}
	}
}`)
	tr.SetData("groups", [][]string{{"x"}, {"y"}})
	tr.SetData("items", []string{"a", "b"})
	tr.SetData("selected", 0)
	tr.ApplyExpressions()
	if got := len(texts(tr)); got != 0 {
		t.Fatalf("selected 0 has %d texts, want 0", got)
	}
	tr.SetData("selected", 1)
	tr.ApplyExpressions()
	if got := texts(tr); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("selected 1 = %v, want [a b]", got)
	}
}

// TestStateModifierPlainElement is the Column example: a plain element follows
// the pointer even though it is not a Material component.
func TestStateModifierPlainElement(t *testing.T) {
	tr := buildModifier(t, `Window {
		Column {
			id:         box
			background: state {
				default: #ff0000ff
				hover:   #00ff00ff
				active:  #0000ffff
			}
		}
	}`)
	box := tr.ids["box"].Base()
	read := func() uint32 {
		tr.ApplyExpressions()
		return box.Background()
	}
	if got := read(); got != 0xFF0000FF {
		t.Fatalf("default = %08x", got)
	}
	box.SetStateHover(true)
	if got := read(); got != 0x00FF00FF {
		t.Fatalf("hover = %08x", got)
	}
	box.SetStatePressed(true)
	if got := read(); got != 0x0000FFFF {
		t.Fatalf("active, which is written last, = %08x", got)
	}
	box.SetStateHover(false)
	box.SetStatePressed(false)
	if got := read(); got != 0xFF0000FF {
		t.Fatalf("back to default = %08x", got)
	}
}

// TestStateModifierControlStates covers checked, disabled and focus on the
// components that own those states.
func TestStateModifierControlStates(t *testing.T) {
	tr := buildModifier(t, `Window {
		Switch {
			id:         sw
			checked:    true
			background: state { default: #ff0000ff checked: #00ff00ff disabled: #0000ffff }
		}
		TextField {
			id:         field
			background: state { default: #ff0000ff focus: #00ff00ff }
		}
	}`)
	sw := tr.ids["sw"].(*Switch)
	field := tr.ids["field"].(*TextField)
	tr.ApplyExpressions()
	if got := sw.Background(); got != 0x00FF00FF {
		t.Fatalf("checked switch = %08x", got)
	}
	sw.Disabled.Set(true)
	tr.ApplyExpressions()
	if got := sw.Background(); got != 0x0000FFFF {
		t.Fatalf("disabled switch = %08x", got)
	}
	sw.Disabled.Set(false)
	sw.Checked.Set(false)
	tr.ApplyExpressions()
	if got := sw.Background(); got != 0xFF0000FF {
		t.Fatalf("unchecked switch = %08x", got)
	}
	field.Base().SetStateFocused(true)
	tr.ApplyExpressions()
	if got := field.Background(); got != 0x00FF00FF {
		t.Fatalf("focused field = %08x", got)
	}
}

// TestNestedModifiers combines responsive, conditional and state in the two
// orders the language allows.
func TestNestedModifiers(t *testing.T) {
	tr := buildModifier(t, `bridge { on bool }
Window {
	Rectangle {
		id:         box
		background: responsive {
			base: #ff0000ff
			sm: state {
				default: #00ff00ff
				hover:   #0000ffff
			}
		}
	}
	Rectangle {
		id:         second
		background: conditional {
			bridge.on: state { default: #111111ff hover: #222222ff }
			default:   #333333ff
		}
	}
}`)
	box := tr.ids["box"].Base()
	second := tr.ids["second"].Base()
	read := func(width float64) {
		tr.ApplyBreakpoints(width)
		tr.ApplyExpressions()
	}
	read(800)
	if got := box.Background(); got != 0x00FF00FF {
		t.Fatalf("responsive state default = %08x", got)
	}
	box.SetStateHover(true)
	read(800)
	if got := box.Background(); got != 0x0000FFFF {
		t.Fatalf("responsive state hover = %08x", got)
	}
	read(300)
	if got := box.Background(); got != 0xFF0000FF {
		t.Fatalf("below sm = %08x", got)
	}

	read(800)
	if got := second.Background(); got != 0x333333FF {
		t.Fatalf("conditional default = %08x", got)
	}
	tr.SetData("on", true)
	read(800)
	if got := second.Background(); got != 0x111111FF {
		t.Fatalf("conditional state default = %08x", got)
	}
	second.SetStateHover(true)
	read(800)
	if got := second.Background(); got != 0x222222FF {
		t.Fatalf("conditional state hover = %08x", got)
	}
}
