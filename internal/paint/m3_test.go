package paint

import (
	"math"
	"testing"
	"time"

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

// TestSliderCenteredFillsFromCenter keeps the centered variant honest: the
// active fill starts at the center value, not at the track's start.
func TestSliderCenteredFillsFromCenter(t *testing.T) {
	col := m3.Light().Colors
	activeOf := func(src string) (start, end float64) {
		l := m3list(t, src)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Color == col.Primary && op.Rect[3] == 16 && op.Rect[2] < 150 {
				return float64(op.Rect[0]), float64(op.Rect[0] + op.Rect[2])
			}
		}
		t.Fatalf("no active segment:\n%s", l.Dump())
		return 0, 0
	}
	// Track spans 2..198 on a 200dp slider: the midpoint is 100.
	start, end := activeOf(`Window { Slider { variant: centered value: 0.8 min: 0 max: 1 width: 200 } }`)
	if start < 99 || start > 101 {
		t.Fatalf("fill starts at %v, want the midpoint", start)
	}
	if end > 160 {
		t.Fatalf("fill ends at %v, want it short of the handle at 160", end)
	}
	// center: 0.25 moves the origin.
	start, _ = activeOf(`Window { Slider { variant: centered center: 0.25 value: 0.8 min: 0 max: 1 width: 200 } }`)
	if start < 48 || start > 52 {
		t.Fatalf("fill starts at %v, want the declared center at 50", start)
	}
}

// TestSliderRangePaintsTwoHandles checks the range variant: two handles, an
// active fill between them, and a stop dot at both ends.
func TestSliderRangePaintsTwoHandles(t *testing.T) {
	col := m3.Light().Colors
	l := m3list(t, `Window { Slider { variant: range value: 0.2 end: 0.7 min: 0 max: 1 width: 200 } }`)
	handles, dots := 0, 0
	var active [4]float32
	for _, op := range l.Ops {
		switch {
		case op.Kind == Fill && op.Color == col.Primary && op.Rect[2] == 4 && op.Rect[3] == 44:
			handles++
		case op.Kind == Fill && op.Shape == Circle && op.Rect[2] == 4:
			dots++
		case op.Kind == Fill && op.Color == col.Primary && op.Rect[3] == 16:
			active = op.Rect
		}
	}
	if handles != 2 {
		t.Fatalf("range handles = %d, want 2\n%s", handles, l.Dump())
	}
	if dots != 2 {
		t.Fatalf("range stop dots = %d, want 2", dots)
	}
	// 0.2 and 0.7 of the 2..198 track are 41.2 and 139.2; the fill runs
	// between them with a gap on each side.
	if active[0] < 41 || active[0] > 50 || active[0]+active[2] > 140 {
		t.Fatalf("range fill = %v, want it between the handles", active)
	}
}

// TestProgressVariants covers the two looks: a normal bar is two rounded rects
// and the stop dot, while the wavy one is a ribbon of joined segments that
// starts and ends with a round cap. The wavy ring is a ribbon too.
func TestProgressVariants(t *testing.T) {
	col := m3.Light().Colors
	count := func(l *List, pred func(Op) bool) int {
		n := 0
		for _, op := range l.Ops {
			if pred(op) {
				n++
			}
		}
		return n
	}
	rotated := func(op Op) bool { return op.Kind == Fill && op.Rotate != 0 }
	caps := func(op Op) bool { return op.Kind == Fill && op.Shape == Circle }
	stop := func(op Op) bool {
		return op.Kind == Fill && math.Abs(float64(op.Rect[2])-4) < 0.01 && math.Abs(float64(op.Rect[3])-4) < 0.01
	}

	normal := m3list(t, `Window { LinearProgress { value: 0.6 width: 300 } }`)
	if got := count(normal, rotated); got != 0 {
		t.Fatalf("a normal bar drew %d wave segments", got)
	}
	if got := count(normal, stop); got != 1 {
		t.Fatalf("a normal bar drew %d stop dots, want 1", got)
	}

	wavy := m3list(t, `Window { LinearProgress { variant: wavy value: 0.6 width: 300 } }`)
	if got := count(wavy, rotated); got < 40 {
		t.Fatalf("a wavy bar drew %d wave segments", got)
	}
	if got := count(wavy, caps); got != 2 {
		t.Fatalf("a wavy bar drew %d caps, want 2", got)
	}
	if got := count(wavy, stop); got != 1 {
		t.Fatalf("a wavy bar drew %d stop dots, want 1", got)
	}

	ring := m3list(t, `Window { CircularProgress { variant: wavy value: 0.6 } }`)
	if got := count(ring, rotated); got < 20 {
		t.Fatalf("a wavy ring drew %d wave segments", got)
	}
	for _, op := range ring.Ops {
		if op.Kind == Fill && op.Color == col.SecondaryContainer {
			return // the plain track is still drawn under the ribbon
		}
	}
	t.Fatal("a wavy ring lost its track")
}

// TestProgressThickness checks the stroke is the design's to choose: a normal
// bar draws its line at the thickness it was given, and a wavy one scales its
// ribbon with it.
func TestProgressThickness(t *testing.T) {
	height := func(op Op) float64 { return float64(op.Rect[3]) }
	normal := m3list(t, `Window { LinearProgress { value: 0.5 width: 300 thickness: 8 } }`)
	found := false
	for _, op := range normal.Ops {
		if op.Kind == Fill && op.Color == m3.Light().Colors.Primary && math.Abs(height(op)-8) < 0.01 {
			found = true
		}
	}
	if !found {
		t.Fatalf("a thickness of 8 did not reach the bar\n%s", normal.Dump())
	}

	// A wavy ribbon carries its stroke on the segments, which are as wide as the
	// thickness and turned with the wave.
	wavy := m3list(t, `Window { LinearProgress { variant: wavy value: 0.5 width: 300 thickness: 12 } }`)
	maxW := 0.0
	for _, op := range wavy.Ops {
		if op.Kind == Fill && op.Shape == RoundedRect && op.Rotate != 0 {
			if w := float64(op.Rect[3]); w > maxW {
				maxW = w
			}
		}
	}
	if maxW < 11 {
		t.Fatalf("a wavy thickness of 12 drew segments %.1f wide", maxW)
	}

	// A ring's stroke is the thickness too, on the plain arc.
	ring := m3list(t, `Window { CircularProgress { value: 0.5 thickness: 10 } }`)
	for _, op := range ring.Ops {
		if op.Kind == Fill && op.Color == m3.Light().Colors.Primary && math.Abs(float64(op.Thickness)-10) < 0.01 {
			return
		}
	}
	t.Fatalf("a ring ignored its thickness\n%s", ring.Dump())
}

// TestWavyProgressAnimates is what makes the wave move: the same design at two
// clock values draws the ribbon at different points, and paint says a frame is
// needed while it does.
func TestWavyProgressAnimates(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { LinearProgress { variant: wavy value: 0.5 width: 300 } }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	l := &List{}
	l.Build(tr, 400, 400)
	if !l.Animating {
		t.Fatal("a wavy bar did not ask for the next frame")
	}
	first := l.Dump()
	l.Reset()
	l.Clock = 400 * time.Millisecond
	l.Build(tr, 400, 400)
	if first == l.Dump() {
		t.Fatal("the wave did not move with the clock")
	}
}

// TestProgressHeightFollowsThickness is the layout half: a thicker stroke needs
// a taller box, and a wavy one needs the ribbon's height rather than the line's.
func TestProgressHeightFollowsThickness(t *testing.T) {
	height := func(src string) float64 {
		tr, diag := tree.BuildSource([]byte(src), nil)
		if !diag.Empty() {
			t.Fatalf("%v", diag)
		}
		layout.New(nil).Layout(tr.Root, 400, 400)
		for _, n := range tr.All() {
			if n.Kind() == "LinearProgress" {
				return n.Base().Bounds().H
			}
		}
		t.Fatal("no bar")
		return 0
	}
	if got := height(`Window { Column { LinearProgress { width: 300 } } }`); math.Abs(got-4) > 0.01 {
		t.Fatalf("a normal bar is %.1f tall, want 4", got)
	}
	if got := height(`Window { Column { LinearProgress { width: 300 thickness: 10 } } }`); math.Abs(got-10) > 0.01 {
		t.Fatalf("a 10dp bar is %.1f tall", got)
	}
	if got := height(`Window { Column { LinearProgress { width: 300 variant: wavy } } }`); math.Abs(got-10.5) > 0.01 {
		t.Fatalf("a wavy bar is %.1f tall, want 10.5 (6dp ribbon)", got)
	}
	if got := height(`Window { Column { LinearProgress { width: 300 variant: wavy thickness: 12 } } }`); math.Abs(got-21) > 0.01 {
		t.Fatalf("a 12dp wavy bar is %.1f tall, want 21", got)
	}
}

// TestProgressGapNeverCloses is the look the design asks for: the filled part
// and the track never touch. A bar keeps a gap the width of its stroke; a ring's
// gap has to carry the two round caps as well, which is what closed it before.
func TestProgressGapNeverCloses(t *testing.T) {
	for _, thickness := range []string{"", "thickness: 10"} {
		// The bar: the active rect's right edge to the track's left edge.
		l := m3list(t, `Window { Column { LinearProgress { value: 0.4 width: 300 `+thickness+` } } }`)
		var activeRight, trackLeft float64
		for _, op := range l.Ops {
			switch {
			case op.Kind != Fill || op.Shape != RoundedRect || op.Rect[2] < 20:
			case op.Color == m3.Light().Colors.Primary:
				activeRight = float64(op.Rect[0] + op.Rect[2])
			case op.Color == m3.Light().Colors.SecondaryContainer:
				trackLeft = float64(op.Rect[0])
			}
		}
		if gap := trackLeft - activeRight; gap < 2.99 {
			t.Fatalf("%s: the bar's gap is %.2fdp", thickness, gap)
		}

		// The ring: the angle between the two arcs, less the cap each band draws.
		l = m3list(t, `Window { CircularProgress { value: 0.4 `+thickness+` } }`)
		var arcs []Op
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Shape == Arc {
				arcs = append(arcs, op)
			}
		}
		if len(arcs) != 2 {
			t.Fatalf("%s: %d arcs, want the value and the track", thickness, len(arcs))
		}
		mid := float64(arcs[0].Rect[2])/2 - float64(arcs[0].Thickness)/2
		edge := (float64(arcs[1].StartAngle)-float64(arcs[0].EndAngle))*mid - float64(arcs[0].Thickness)
		if edge < 2.99 {
			t.Fatalf("%s: the ring's gap is %.2fdp", thickness, edge)
		}
		// And the gap at the other end, between the track's end and 12 o'clock.
		start := 2*math.Pi - float64(arcs[1].EndAngle)
		if edge2 := start*mid - float64(arcs[0].Thickness); edge2 < 2.99 {
			t.Fatalf("%s: the ring's second gap is %.2fdp", thickness, edge2)
		}
	}
}

// TestRadiusClampsToHalfTheBox is the circle case: a radius past half the box is
// a circle in CSS, and the shader's distance field turns everything outside when
// it is not clamped. The op carries the clamped value so no renderer has to guess.
func TestRadiusClampsToHalfTheBox(t *testing.T) {
	l := m3list(t, `Window { Column {
		Rectangle { width: 12 height: 12 radius: 999 background: #ff0000ff }
		Rectangle { width: 40 height: 20 radius: 999 background: #00ff00ff }
	} }`)
	var radii [][4]float32
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Shape == RoundedRect && op.Rect[3] <= 40 && op.Rect[3] > 5 {
			radii = append(radii, op.Radius)
		}
	}
	if len(radii) != 2 {
		t.Fatalf("drew %d boxes, want 2\n%s", len(radii), l.Dump())
	}
	// 12x12 → 6, 40x20 → 10: half the shorter side, never more.
	if radii[0][0] != 6 || radii[1][0] != 10 {
		t.Fatalf("radii = %v, want 6 and 10", radii)
	}
}

// TestCurveStepFollowsTheScale is the adaptive sampling: the same spline is
// sampled finer on a HiDPI panel, because the step is measured in device pixels.
func TestCurveStepFollowsTheScale(t *testing.T) {
	build := func(scale float32) *List {
		tr, diag := tree.BuildSource([]byte(`Window { Column { LineChart { width: 220 height: 240 smooth: true
			categories: ["a", "b", "c", "d"]
			ChartSeries { values: [10, 40, 15, 45] } } } }`), nil)
		if !diag.Empty() {
			t.Fatalf("%v", diag)
		}
		layout.New(nil).Layout(tr.Root, 600, 400)
		l := &List{Scale: scale}
		l.Build(tr, 600, 400)
		return l
	}
	count := func(l *List) int {
		n := 0
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Shape == RoundedRect && op.Rotate != 0 && op.Rect[2] > 2 {
				n++
			}
		}
		return n
	}
	one, two := count(build(1)), count(build(2))
	if one == 0 || two <= one {
		t.Fatalf("segments at 1x = %d, at 2x = %d; want more at 2x", one, two)
	}
}

// TestLoadingIndicatorIsNativeShape covers the loading shape: it is one signed
// distance rather than a bitmap, its colour comes from the component, and its
// turn and morph follow the frame clock.
func TestLoadingIndicatorIsNativeShape(t *testing.T) {
	src := `Window { Column {
		LoadingIndicator { size: 48 }
		LoadingIndicator { size: 96 color: #cc4444ff }
	} }`
	l := m3list(t, src)
	var ops []Op
	for _, op := range l.Ops {
		if op.Shape == Loading {
			ops = append(ops, op)
		}
	}
	if len(ops) != 2 {
		t.Fatalf("drew %d loading shapes, want 2\n%s", len(ops), l.Dump())
	}
	if ops[1].Color != 0xCC4444FF {
		t.Fatalf("colour = %08x, want the component's own", ops[1].Color)
	}
	if ops[1].Rect[2] != 96 || ops[1].Rect[3] != 96 {
		t.Fatalf("size: 96 gave box %v", ops[1].Rect)
	}
	if !l.Animating {
		t.Fatal("a loading shape must keep the frame loop running")
	}
	if ops[0].Depth < 0 || ops[0].Depth > 1 {
		t.Fatalf("depth = %v, want 0..1", ops[0].Depth)
	}
	if ops[0].Square < 0 || ops[0].Square > 1 || ops[0].Squash < 0 || ops[0].Squash > 0.5 {
		t.Fatalf("morph = square %v squash %v, want 0..1 and a mild squeeze",
			ops[0].Square, ops[0].Squash)
	}
	// The square phase is the one that squeezes into the pill, so a squeeze
	// without squareness would be a shape the reference never draws.
	for _, op := range ops {
		if op.Squash > 0 && op.Square <= 0 {
			t.Fatalf("squash %v at squareness %v", op.Squash, op.Square)
		}
	}

	// A later frame has turned the shape and moved its morph on.
	first := ops[0]
	later := listAt(t, src, 400, 400, 900*time.Millisecond)
	for _, op := range later.Ops {
		if op.Shape == Loading {
			if op.StartAngle == first.StartAngle || op.Depth == first.Depth {
				t.Fatalf("frame at 900ms: turn %v depth %v, want both moved from %v/%v",
					op.StartAngle, op.Depth, first.StartAngle, first.Depth)
			}
			return
		}
	}
	t.Fatal("no loading shape in the later frame")
}

// TestTooltipIsAContainer pins the tooltip contract: the box is exactly its
// child, nothing is drawn at rest, and hovering the child brings a bubble above
// it that is not part of the layout.
func TestTooltipIsAContainer(t *testing.T) {
	// The padding leaves room above, which is where a tooltip belongs.
	const src = `Window { Column {
		padding: 60
		Tooltip { id: tip text: "Saves to your library"
			Text { id: label text: "Hover me" }
		}
	} }`
	build := func(hover bool) (*List, tree.Rect) {
		tr, diag := tree.BuildSource([]byte(src), nil)
		if !diag.Empty() {
			t.Fatalf("%v", diag)
		}
		layout.New(nil).Layout(tr.Root, 400, 400)
		n, _ := tr.ByID("tip")
		kid, _ := tr.ByID("label")
		if n == nil || kid == nil {
			t.Fatal("nodes missing")
		}
		nb, kb := n.Base().Bounds(), kid.Base().Bounds()
		if nb.W != kb.W || nb.H != kb.H {
			t.Fatalf("tooltip %v does not wrap its child %v", nb, kb)
		}
		if hover {
			n.(*tree.Tooltip).M3().Hover = true
		}
		l := &List{}
		l.Build(tr, 400, 400)
		return l, n.Base().Bounds()
	}
	rest, _ := build(false)
	if n := countBubble(rest); n != 0 {
		t.Fatalf("bubble drawn without hover: %d fills\n%s", n, rest.Dump())
	}
	hovered, wrapped := build(true)
	if n := countBubble(hovered); n != 1 {
		t.Fatalf("bubbles on hover = %d, want 1\n%s", n, hovered.Dump())
	}
	box := bubbleBox(hovered)
	if box.Y+box.H > wrapped.Y+1 {
		t.Fatalf("bubble %v does not sit above the child %v", box, wrapped)
	}
}

func countBubble(l *List) int {
	n := 0
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == m3.Light().Colors.InverseSurface {
			n++
		}
	}
	return n
}

func bubbleBox(l *List) tree.Rect {
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == m3.Light().Colors.InverseSurface {
			return tree.Rect{X: float64(op.Rect[0]), Y: float64(op.Rect[1]),
				W: float64(op.Rect[2]), H: float64(op.Rect[3])}
		}
	}
	return tree.Rect{}
}
