package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// dump builds the list and returns it, failing on a parse error.
func m3list(t *testing.T, src string) *List {
	t.Helper()
	return list(t, src, 400, 400)
}

// TestButtonPaintsFilledContainer checks the M3 button contract: a primary
// container, a state layer when hovered, and a centered label.
func TestButtonPaintsFilledContainer(t *testing.T) {
	l := m3list(t, `Window { Button { label: "Go" } }`)
	col := m3.Light().Colors
	var bg, label bool
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == col.Primary {
			bg = true
		}
		if op.Kind == Text && op.Text == "Go" && op.Color == col.OnPrimary {
			label = true
		}
	}
	if !bg || !label {
		t.Fatalf("button ops missing: bg=%v label=%v\n%s", bg, label, l.Dump())
	}
}

// TestDisabledButtonLabelStaysReadable guards the M3 disabled treatment. The
// label used to be OnPrimary at 38% over a 12% OnPrimary container, and then
// dimmed a second time to 14%, which is unreadable on a light page.
func TestDisabledButtonLabelStaysReadable(t *testing.T) {
	l := m3list(t, `Window { Button { label: "Go" disabled: true } }`)
	col := m3.Light().Colors
	wantBg := withAlpha(col.OnSurface, m3.DisabledContainerOpacity)
	wantLabel := withAlpha(col.OnSurface, m3.DisabledOpacity)
	var bg, label bool
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == wantBg {
			bg = true
		}
		if op.Kind == Text && op.Text == "Go" && op.Color == wantLabel {
			label = true
		}
	}
	if !bg || !label {
		t.Fatalf("disabled button ops: bg=%v label=%v\n%s", bg, label, l.Dump())
	}
}

func TestButtonVariants(t *testing.T) {
	for _, tc := range []struct {
		variant string
		want    uint32
	}{
		{"filled", m3.Light().Colors.Primary},
		{"tonal", m3.Light().Colors.SecondaryContainer},
		{"outlined", 0},
	} {
		l := m3list(t, `Window { Button { label: "Go" variant: `+tc.variant+` } }`)
		if len(l.Ops) == 0 || l.Ops[0].Color != tc.want {
			t.Fatalf("%s: first op color = %08x, want %08x", tc.variant, l.Ops[0].Color, tc.want)
		}
	}
}

func TestSliderDrawsExpressiveTrack(t *testing.T) {
	l := m3list(t, `Window { Slider { value: 0.5 min: 0 max: 1 width: 200 } }`)
	col := m3.Light().Colors
	var active, inactive, handle, stop [4]float32
	var activeRadii, inactiveRadii [4]float32
	for _, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		switch {
		case op.Color == col.Primary && op.Rect[3] == 16:
			active, activeRadii = op.Rect, op.Radius
		case op.Color == col.SecondaryContainer && op.Rect[3] == 16:
			inactive, inactiveRadii = op.Rect, op.Radius
		case op.Color == col.Primary && op.Rect[2] == 4 && op.Rect[3] == 44:
			handle = op.Rect
		case op.Shape == Circle && op.Rect[2] == 4 && op.Color == col.Primary:
			stop = op.Rect
		}
	}
	if active[2] == 0 || inactive[2] == 0 || handle[2] == 0 || stop[2] == 0 {
		t.Fatalf("slider parts missing:\n%s", l.Dump())
	}
	if active[3] != 16 || inactive[3] != 16 || handle[3] != 44 {
		t.Fatalf("sizes active=%v inactive=%v handle=%v", active, inactive, handle)
	}
	if active[0]+active[2] != handle[0]-6 {
		t.Fatalf("active track does not stop 6dp before the handle: %v / %v", active, handle)
	}
	if inactive[0] != handle[0]+handle[2]+6 {
		t.Fatalf("inactive track does not start 6dp after the handle: %v / %v", inactive, handle)
	}
	if activeRadii[0] != 8 || activeRadii[1] != 2 || inactiveRadii[0] != 2 || inactiveRadii[1] != 8 {
		t.Fatalf("track radii active=%v inactive=%v", activeRadii, inactiveRadii)
	}
	if stop[0]+2 != inactive[0]+inactive[2]-8 {
		t.Fatalf("stop dot = %v, want 4dp at 8dp from the inactive end", stop)
	}
}

func TestSliderHandleNarrowsWhenPressed(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Slider { value: 0.5 width: 200 } }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	for _, n := range tr.All() {
		if n.Kind() == "Slider" {
			n.(*tree.Slider).M3().Pressed = true
		}
	}
	l := &List{}
	l.Build(tr, 400, 400)
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == m3.Light().Colors.Primary && op.Rect[3] == 44 {
			if op.Rect[2] != 2 {
				t.Fatalf("pressed handle width = %v, want 2", op.Rect[2])
			}
			return
		}
	}
	t.Fatalf("no handle:\n%s", l.Dump())
}

func TestCheckboxDrawsCheckWhenChecked(t *testing.T) {
	l := m3list(t, `Window { Checkbox { checked: true } }`)
	var check, box bool
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "check" {
			check = true
		}
		if op.Kind == Fill && op.Color == m3.Light().Colors.Primary {
			box = true
		}
	}
	if !check || !box {
		t.Fatalf("checked checkbox ops: check=%v box=%v\n%s", check, box, l.Dump())
	}
}

// TestDisabledDimmsEveryPaint is the guard that a disabled control never looks
// enabled: the fill is translucent.
func TestDisabledDimmsEveryPaint(t *testing.T) {
	l := m3list(t, `Window { Button { label: "Go" disabled: true } }`)
	if len(l.Ops) == 0 {
		t.Fatal("no ops")
	}
	if a := l.Ops[0].Color & 0xFF; a > 0x40 {
		t.Fatalf("disabled fill alpha = %d, want low", a)
	}
}

// TestNavigationItemStacksInWideBar guards the arrangement rule: a wide
// destination in a navigation bar still puts the label under the icon.
func TestNavigationItemStacksInWideBar(t *testing.T) {
	l := m3list(t, `Window { NavigationBar { NavigationItem { label: "Home" icon: home } } }`)
	var icon, label Op
	for _, op := range l.Ops {
		if op.Kind != Text {
			continue
		}
		if op.Icon {
			icon = op
		} else {
			label = op
		}
	}
	if icon.Text == "" || label.Text == "" {
		t.Fatalf("missing ops:\n%s", l.Dump())
	}
	if icon.Rect[1] >= label.Rect[1] {
		t.Fatalf("icon y=%v, label y=%v: want icon above label", icon.Rect[1], label.Rect[1])
	}
}

// TestRippleStaysInsideTheComponent guards the press feedback box: the disc is
// wider than a pill while it expands, so clipping it to its ancestors (the
// page) let it spill outside the button.
func TestRippleStaysInsideTheComponent(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			padding: 40
			Button { id: pill width: 200 height: 56 label: "Go" }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("pill")
	r := n.Base().Bounds()
	c := n.(*tree.Button).M3()
	c.RippleOn, c.Ripple, c.Pressed = true, 0.4, true
	c.RippleX, c.RippleY = r.X+r.W/2, r.Y+r.H/2

	l := &List{}
	l.Build(tr, 400, 400)
	var found bool
	for _, op := range l.Ops {
		if op.Shape != Circle || op.ClipRadius == 0 {
			continue
		}
		found = true
		want := [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)}
		if op.Clip != want {
			t.Fatalf("ripple clip = %v, want the button box %v", op.Clip, want)
		}
	}
	if !found {
		t.Fatalf("no clipped ripple op:\n%s", l.Dump())
	}
}

// TestTextFieldSelectionIsHighlighted guards the selection band: it must sit
// behind the value, tinted with the primary color, from the anchor to the caret.
func TestTextFieldSelectionIsHighlighted(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		TextField { id: field label: "Name" width: 240 }
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("field")
	c := n.(*tree.TextField).M3()
	c.Text.Set("hello")
	c.Focused = true
	c.SelAnchor, c.Caret = 1, 4

	l := &List{}
	l.Build(tr, 400, 400)
	want := withAlpha(m3.Light().Colors.Primary, m3.SelectionOpacity)
	for _, op := range l.Ops {
		if op.Shape == RoundedRect && op.Color == want {
			return
		}
	}
	t.Fatalf("no selection band:\n%s", l.Dump())
}

// TestPreeditIsDrawnUnderlined checks IME composition is painted after the
// value with an underline, so provisional text reads differently from committed
// text.
func TestPreeditIsDrawnUnderlined(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		TextField { id: field width: 240 height: 56 }
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("field")
	c := n.(*tree.TextField).M3()
	c.Text.Set("ab")
	c.Caret = 2
	c.Focused = true
	c.SetPreedit("にほん", -1)

	l := &List{}
	l.Build(tr, 400, 400)
	var text, rule bool
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "にほん" {
			text = true
		}
		if op.Kind == Fill && op.Rect[3] == 1 {
			rule = true
		}
	}
	if !text || !rule {
		t.Fatalf("preedit text=%v underline=%v\n%s", text, rule, l.Dump())
	}
}

// TestWrappedTextDrawsOneOpPerLine checks wrap:true breaks the value into lines
// that stack by the role's line height.
func TestWrappedTextDrawsOneOpPerLine(t *testing.T) {
	l := m3list(t, `Window {
		Column {
			Text {
				width: 200
				wrap: true
				text: "Narrow column, wrapping on. Long paragraphs break at word boundaries and the element reports the height of every line it needs."
			}
		}
	}`)
	var ys []float32
	for _, op := range l.Ops {
		if op.Kind == Text {
			ys = append(ys, op.TextY)
		}
	}
	if len(ys) < 3 {
		t.Fatalf("lines = %d, want the paragraph broken up:\n%s", len(ys), l.Dump())
	}
	// bodyMedium has a 20dp line box, so baselines are 20dp apart.
	for i := 1; i < len(ys); i++ {
		if d := ys[i] - ys[i-1]; d < 19.9 || d > 20.1 {
			t.Fatalf("baseline gap %v, want 20", d)
		}
	}
}

// TestWrappedTextReservesItsHeight checks layout and paint agree: the element is
// as tall as the number of lines paint emits.
func TestWrappedTextReservesItsHeight(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			Text {
				id: para
				width: 200
				wrap: true
				text: "Narrow column, wrapping on. Long paragraphs break at word boundaries and the element reports the height of every line it needs."
			}
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("para")
	box := n.Base().Bounds()

	l := &List{}
	l.Build(tr, 400, 400)
	lines := 0
	for _, op := range l.Ops {
		if op.Kind == Text {
			lines++
		}
	}
	if want := float64(lines) * 20; box.H != want {
		t.Fatalf("reserved height = %v for %d lines, want %v", box.H, lines, want)
	}
}

// TestWrapFalseStaysOnOneLine is the contrast case: without wrap the value is a
// single run that the parent clips.
func TestWrapFalseStaysOnOneLine(t *testing.T) {
	l := m3list(t, `Window {
		Column {
			Text { width: 120 text: "this line is far wider than the column" }
		}
	}`)
	lines := 0
	for _, op := range l.Ops {
		if op.Kind == Text {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("lines = %d, want 1", lines)
	}
}

// TestDeclaredWeightSnapsToALoadedFace guards the loaded Roboto weights: a value
// between them draws with the nearest, so layout and paint use the same face.
func TestDeclaredWeightSnapsToALoadedFace(t *testing.T) {
	l := m3list(t, `Window {
		Column {
			Text { width: 300 fontSize: 20 weight: 600 text: "semibold snaps to bold" }
		}
	}`)
	for _, op := range l.Ops {
		if op.Kind == Text {
			if op.Weight != 700 {
				t.Fatalf("weight = %v, want 700 (nearest loaded face)", op.Weight)
			}
			return
		}
	}
	t.Fatalf("no text op:\n%s", l.Dump())
}

// TestTextFieldScrollsInsteadOfOverflowing checks a focused field keeps the
// caret in view by scrolling the value, and clips glyphs to the text box.
func TestTextFieldScrollsInsteadOfOverflowing(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		TextField { id: field label: "Name" width: 200 }
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("field")
	c := n.(*tree.TextField).M3()
	long := "a very long value that cannot fit inside one field"
	c.Text.Set(long)
	c.Caret = len(long)

	// Unfocused: the start of the value is shown.
	l := &List{}
	l.Build(tr, 400, 400)
	if c.TextScroll != 0 {
		t.Fatalf("unfocused scroll = %v, want 0", c.TextScroll)
	}

	// Focused: the value scrolls left and glyphs are clipped to the text box.
	c.Focused = true
	l.Build(tr, 400, 400)
	if c.TextScroll <= 0 {
		t.Fatalf("focused scroll = %v, want the value moved left", c.TextScroll)
	}
	var value Op
	found := false
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == long {
			value, found = op, true
		}
	}
	if !found {
		t.Fatalf("no value op:\n%s", l.Dump())
	}
	if value.Clip == [4]float32{0, 0, 400, 400} {
		t.Fatalf("value clip = %v, want the field's text box", value.Clip)
	}
	// The end of the value (where the caret sits) stays inside that box.
	caretX := value.TextX + float32(textWidth(l, long, m3.Light().Type.BodyLarge))
	if caretX > value.Clip[0]+value.Clip[2]+0.5 {
		t.Fatalf("caret x = %v past the text box %v", caretX, value.Clip)
	}
}

// TestSearchBarSelectionIsHighlighted mirrors the text field: a search bar is
// editable, so a drag selection must paint the same band.
func TestSearchBarSelectionIsHighlighted(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		SearchBar { id: search text: "hello world" width: 300 }
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("search")
	c := n.(*tree.SearchBar).M3()
	c.Focused = true
	c.SelAnchor, c.Caret = 0, 5

	l := &List{}
	l.Build(tr, 400, 400)
	want := withAlpha(m3.Light().Colors.Primary, m3.SelectionOpacity)
	for _, op := range l.Ops {
		if op.Shape == RoundedRect && op.Color == want && op.Rect[2] > 0 {
			return
		}
	}
	t.Fatalf("no selection band:\n%s", l.Dump())
}

// TestSplitButtonPaintsTwoPills guards the Expressive shape: a leading pill and
// a square menu pill with a 2dp gap, not one container with a divider.
func TestSplitButtonPaintsTwoPills(t *testing.T) {
	l := m3list(t, `Window { SplitButton { label: "Split" icon: add width: 180 height: 40 } }`)
	primary := m3.Light().Colors.Primary
	var segs [][4]float32
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == primary && op.Rect[3] > 0 {
			segs = append(segs, op.Rect)
		}
	}
	if len(segs) != 2 {
		t.Fatalf("segments = %d, want 2:\n%s", len(segs), l.Dump())
	}
	main, trail := segs[0], segs[1]
	if gap := trail[0] - (main[0] + main[2]); gap != 2 {
		t.Fatalf("gap = %v, want 2", gap)
	}
	if main[3] != trail[3] || trail[2] != 48 {
		t.Fatalf("segments = %v / %v, want equal height and a 48dp menu segment", main, trail)
	}
}

// TestSplitButtonRadiusIsStable guards the hover bug: only the state layer may
// react, the pill shape itself must not change while hovered or pressed.
func TestSplitButtonRadiusIsStable(t *testing.T) {
	radiiFor := func(hover, pressed bool) [][4]float32 {
		tr, diag := tree.BuildSource([]byte(
			`Window { SplitButton { label: "Split" width: 180 height: 40 } }`,
		), nil)
		if !diag.Empty() {
			t.Fatalf("%v", diag)
		}
		layout.New(nil).Layout(tr.Root, 400, 400)
		for _, n := range tr.All() {
			if n.Kind() == "SplitButton" {
				c := n.(*tree.SplitButton).M3()
				c.Hover, c.Pressed = hover, pressed
			}
		}
		l := &List{}
		l.Build(tr, 400, 400)
		var radii [][4]float32
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Color == m3.Light().Colors.Primary {
				radii = append(radii, op.Radius)
			}
		}
		return radii
	}
	want := radiiFor(false, false)
	for _, state := range []struct{ hover, pressed bool }{{true, false}, {false, true}} {
		got := radiiFor(state.hover, state.pressed)
		if len(got) != len(want) {
			t.Fatalf("segments = %d, want %d", len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("hover=%v pressed=%v radii = %v, want %v",
					state.hover, state.pressed, got, want)
			}
		}
	}
}

// TestFlowContainersDoNotClipShadows guards the shadow fix: a Row sizes to its
// content, so clipping children to its box cut the shadow of the first and last
// button. Only surfaces and scroll viewports clip.
func TestFlowContainersDoNotClipShadows(t *testing.T) {
	l := m3list(t, `Window {
		Column {
			crossAxisAlign: start
			Row {
				Button { label: "x" variant: elevated }
			}
		}
	}`)
	for _, op := range l.Ops {
		if op.Kind != Shadow {
			continue
		}
		if want := [4]float32{0, 0, 400, 400}; op.Clip != want {
			t.Fatalf("shadow clip = %v, want %v", op.Clip, want)
		}
		return
	}
	t.Fatalf("no shadow op:\n%s", l.Dump())
}

// TestFocusedButtonDrawsRing checks that keyboard focus is visible: a Primary
// outline around the component.
func TestFocusedButtonDrawsRing(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Button { id: a label: "Go" } }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	btn, _ := tr.ByID("a")
	col := m3.Light().Colors
	focused := func() bool {
		l := &List{}
		l.Build(tr, 400, 400)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Color == 0 && op.BorderColor == col.Primary && op.BorderWidth == 2 {
				return true
			}
		}
		return false
	}
	if focused() {
		t.Fatal("an unfocused button has a ring")
	}
	btn.(*tree.Button).M3().Focused = true
	if focused() {
		t.Fatal("a pointer-focused button drew the keyboard ring")
	}
	btn.(*tree.Button).M3().FocusRing = true
	if !focused() {
		t.Fatal("a keyboard-focused button has no ring")
	}
}

// TestTextDecorations checks that each decoration keyword paints a stroke and
// that `none` paints nothing, and that the style changes the shape drawn.
func TestTextDecorations(t *testing.T) {
	count := func(src string) int {
		l := m3list(t, src)
		n := 0
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Shape != Circle && op.Color == m3.Light().Colors.OnSurface {
				n++
			}
		}
		return n
	}
	if got := count(`Window { Text { text: "x" decoration: none } }`); got != 0 {
		t.Fatalf("none painted %d strokes", got)
	}
	if got := count(`Window { Text { text: "x" decoration: underline overline } }`); got != 2 {
		t.Fatalf("combined decorations painted %d strokes, want 2", got)
	}
	// A wavy stroke is one Wave op; solid is a fill.
	l := m3list(t, `Window { Text { text: "x" decoration: underline decorationStyle: wavy } }`)
	var wave bool
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Shape == Wave {
			wave = true
		}
	}
	if !wave {
		t.Fatalf("no wave op:\n%s", l.Dump())
	}
	// Colour and thickness feed the stroke.
	l = m3list(t, `Window { Text { text: "x" decoration: underline decorationThickness: 4 decorationColor: #FF0000 } }`)
	var found bool
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == 0xFF0000FF && op.Rect[3] == 4 {
			found = true
		}
	}
	if !found {
		t.Fatalf("decoration colour/thickness not applied:\n%s", l.Dump())
	}
}

// TestTextItalicAndRelativeWeight checks that italic reaches the op and that
// bolder/lighter resolve against the role instead of staying negative.
func TestTextItalicAndRelativeWeight(t *testing.T) {
	l := m3list(t, `Window {
		Text { id: i text: "x" fontStyle: italic }
		Text { id: b text: "x" role: bodyMedium fontWeight: bolder }
		Text { id: l text: "x" role: bodyMedium fontWeight: lighter }
	}`)
	var italic bool
	var weights []float32
	for _, op := range l.Ops {
		if op.Kind != Text {
			continue
		}
		if op.Italic {
			italic = true
		}
		weights = append(weights, op.Weight)
	}
	if !italic {
		t.Fatalf("italic did not reach the text op:\n%s", l.Dump())
	}
	// bodyMedium is 400: bolder steps to 700, lighter to 300. The library snaps
	// to the loaded faces, so compare against those numbers.
	if len(weights) != 3 || weights[1] != 700 || weights[2] != 300 {
		t.Fatalf("weights = %v, want the italic line then 700, 300", weights)
	}
}

// TestNavigationRailIndicatorFollowsScroll guards a scrolled rail: the item
// paints with the scroll shift, so the selected band must move by the same
// amount or it stays behind at the unscrolled position.
func TestNavigationRailIndicatorFollowsScroll(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { NavigationRail {
		id: rail
		scroll: true
		width: 80
		height: 120
		NavigationItem { id: a label: "A" icon: home }
		NavigationItem { id: b label: "B" icon: home }
		NavigationItem { id: c label: "C" icon: home }
		NavigationItem { id: d label: "D" icon: home selected: true }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	rail, _ := tr.ByID("rail")
	item, _ := tr.ByID("d")
	if rail.Base().ScrollMax() <= 0 {
		t.Fatalf("rail does not overflow: max=%v", rail.Base().ScrollMax())
	}
	rail.Base().SetScrollOffset(40)
	bandY := func() float32 {
		l := &List{}
		l.Build(tr, 400, 400)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Rect[2] == 56 && op.Rect[3] == 32 {
				return op.Rect[1]
			}
		}
		t.Fatalf("no indicator band:\n%s", l.Dump())
		return 0
	}
	shifted := bandY()
	rail.Base().SetScrollOffset(0)
	plain := bandY()
	if plain-shifted != 40 {
		t.Fatalf("indicator moved %v, want 40 (scrolled y=%v, rest y=%v)", plain-shifted, shifted, plain)
	}
	if item.Base().Bounds().Y == 0 {
		t.Fatal("selected item sits at the origin; test proves nothing")
	}
}
