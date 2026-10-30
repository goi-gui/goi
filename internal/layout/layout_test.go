package layout

import (
	"testing"

	"github.com/ksckaan1/goi/internal/syntax"
	"github.com/ksckaan1/goi/internal/tree"
)

func build(t *testing.T, src string) *tree.Tree {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	return tr
}

// bounds returns the rect of the node with the given id.
func bounds(t *testing.T, tr *tree.Tree, id string) tree.Rect {
	t.Helper()
	n, ok := tr.ByID(id)
	if !ok {
		t.Fatalf("no node %q", id)
	}
	return n.Base().Bounds()
}

func want(t *testing.T, got tree.Rect, x, y, w, h float64) {
	t.Helper()
	const eps = 0.001
	if abs(got.X-x) > eps || abs(got.Y-y) > eps || abs(got.W-w) > eps || abs(got.H-h) > eps {
		t.Fatalf("rect = %+v, want {%v %v %v %v}", got, x, y, w, h)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestComponentVisible(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Button { id: b label: "hidden" visible: false }
			Text { id: t text: "shown" }
		}
	}`)
	e := New(nil)
	e.Layout(tr.Root, 400, 300)
	b, _ := tr.ByID("b")
	if b.Base().Visible() {
		t.Fatal("button should be hidden")
	}
	if got := bounds(t, tr, "t").Y; got != 0 {
		t.Fatalf("visible text y = %v, want 0", got)
	}

	b.Base().SetVisible(true)
	if _, dirty := b.Base().Dirty(); !dirty {
		t.Fatal("SetVisible should mark the tree dirty")
	}
	e.Layout(tr.Root, 400, 300)
	if !b.Base().Visible() {
		t.Fatal("button should be visible")
	}
	if got := bounds(t, tr, "t").Y; got != 40 {
		t.Fatalf("text y = %v, want 40 below the button", got)
	}
}

func TestResponsiveVisibleOnComponent(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Button { id: b label: "md" visible: { base: true, md: false } }
		}
	}`)
	e := New(nil)
	b, _ := tr.ByID("b")

	tr.ApplyBreakpoints(500)
	e.Layout(tr.Root, 400, 300)
	if !b.Base().Visible() {
		t.Fatal("button should be visible below md")
	}

	tr.ApplyBreakpoints(800)
	e.Layout(tr.Root, 400, 300)
	if b.Base().Visible() {
		t.Fatal("button should be hidden from md up")
	}
}

func TestPercentAndReferenceWidth(t *testing.T) {
	tr := build(t, `Window {
		Column {
			id: col
			padding: 10
			Rectangle { id: a width: 100 height: 10 }
			Rectangle { id: half width: 50% height: 10 }
			Rectangle { id: b width: a.width / 2 height: 10 }
			Rectangle { id: c width: (a.width + b.width) / 3 height: 10 }
			Rectangle { id: d width: a.width-8 height: 10 }
			Rectangle { id: e width: 50% - 10 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 200, 200)
	// Column content width is 180, so 50% is 90.
	want(t, bounds(t, tr, "half"), 10, 20, 90, 10)
	want(t, bounds(t, tr, "b"), 10, 30, 50, 10)
	want(t, bounds(t, tr, "c"), 10, 40, 50, 10)
	want(t, bounds(t, tr, "d"), 10, 50, 92, 10)
	want(t, bounds(t, tr, "e"), 10, 60, 80, 10)
}

func TestPercentHeight(t *testing.T) {
	tr := build(t, `Window {
		Rectangle { id: r width: 10 height: 25% }
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "r"), 0, 0, 10, 50)
}

func TestExpressionFollowsReferenceChange(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Rectangle { id: a width: 100 height: 10 }
			Rectangle { id: b width: a.width / 2 height: 10 }
		}
	}`)
	e := New(nil)
	e.Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "b"), 0, 10, 50, 10)

	a, _ := tr.ByID("a")
	a.Base().SetPropLiteral("width", &syntax.Number{Val: 80})
	e.Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "b"), 0, 10, 40, 10)
}

func TestReferenceCycleDoesNotHang(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Rectangle { id: a width: b.width height: 10 }
			Rectangle { id: b width: a.width height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
}

func TestResponsiveExpressionTier(t *testing.T) {
	tr := build(t, `Window {
		Rectangle { id: a width: 100 height: 10 }
		Rectangle { id: b width: { base: 40, md: a.width / 2 } height: 10 }
	}`)
	e := New(nil)
	tr.ApplyBreakpoints(800)
	e.Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "b"), 0, 0, 50, 10)

	tr.ApplyBreakpoints(500)
	e.Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "b"), 0, 0, 40, 10)
}

func TestColumnStacking(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Text { id: a width: 100 height: 10 }
			Text { id: b width: 50  height: 20 }
			Text { id: c width: 10  height: 30 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "a"), 0, 0, 100, 10)
	want(t, bounds(t, tr, "b"), 0, 10, 50, 20)
	want(t, bounds(t, tr, "c"), 0, 30, 10, 30)
}

func TestColumnSpacingAndPadding(t *testing.T) {
	tr := build(t, `Window {
		Column {
			padding: 10
			spacing: 4
			Text { id: a width: 100 height: 10 }
			Text { id: b width: 100 height: 20 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "a"), 10, 10, 100, 10)
	want(t, bounds(t, tr, "b"), 10, 24, 100, 20)
}

func TestRowHorizontal(t *testing.T) {
	tr := build(t, `Window {
		Row {
			spacing: 5
			Text { id: a width: 40 height: 10 }
			Text { id: b width: 60 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "a"), 0, 0, 40, 10)
	want(t, bounds(t, tr, "b"), 45, 0, 60, 10)
}

func TestCrossAxisAlignment(t *testing.T) {
	tr := build(t, `Window {
		Column {
			align: center
			Text { id: a width: 100 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "a"), 100, 0, 100, 10)

	tr2 := build(t, `Window {
		Column {
			align: end
			Text { id: a width: 100 height: 10 }
		}
	}`)
	New(nil).Layout(tr2.Root, 300, 200)
	want(t, bounds(t, tr2, "a"), 200, 0, 100, 10)
}

func TestAlignSelfOverridesParent(t *testing.T) {
	tr := build(t, `Window {
		Column {
			align: end
			Text { id: a width: 100 height: 10 }
			Text { id: b width: 100 height: 10 alignSelf: start }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "a"), 200, 0, 100, 10)
	want(t, bounds(t, tr, "b"), 0, 10, 100, 10)
}

func TestFlexGrow(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Text { id: fixed width: 10 height: 20 }
			Text { id: grow  width: 10 height: 0 flex: 1 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "fixed"), 0, 0, 10, 20)
	want(t, bounds(t, tr, "grow"), 0, 20, 10, 180)
}

func TestFlexSharesRemainingSpace(t *testing.T) {
	tr := build(t, `Window {
		Row {
			Text { id: a width: 0 height: 10 flex: 1 }
			Text { id: b width: 0 height: 10 flex: 3 }
		}
	}`)
	New(nil).Layout(tr.Root, 400, 200)
	want(t, bounds(t, tr, "a"), 0, 0, 100, 10)
	want(t, bounds(t, tr, "b"), 100, 0, 300, 10)
}

func TestMinMaxWidth(t *testing.T) {
	tr := build(t, `Window {
		Row {
			Text { id: a width: 0 height: 10 flex: 1 minWidth: 200 }
		}
	}`)
	New(nil).Layout(tr.Root, 100, 200)
	want(t, bounds(t, tr, "a"), 0, 0, 200, 10)

	tr2 := build(t, `Window {
		Column {
			Text { id: a width: 900 height: 10 maxWidth: 250 }
		}
	}`)
	New(nil).Layout(tr2.Root, 1000, 200)
	want(t, bounds(t, tr2, "a"), 0, 0, 250, 10)
}

func TestMargin(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Text { id: a width: 100 height: 10 margin: 7 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "a"), 7, 7, 100, 10)
}

func TestStackOverlays(t *testing.T) {
	tr := build(t, `Window {
		Stack {
			Rectangle { id: back }
			Rectangle { id: front width: 50 height: 50 }
		}
	}`)
	New(nil).Layout(tr.Root, 200, 100)
	want(t, bounds(t, tr, "back"), 0, 0, 200, 100)
	want(t, bounds(t, tr, "front"), 0, 0, 50, 50)
}

func TestStackCenter(t *testing.T) {
	tr := build(t, `Window {
		Stack {
			align: center
			Rectangle { id: box width: 40 height: 20 }
		}
	}`)
	New(nil).Layout(tr.Root, 200, 100)
	want(t, bounds(t, tr, "box"), 80, 40, 40, 20)
}

func TestInvisibleExcludedFromLayout(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Text { id: hidden width: 10 height: 100 visible: false }
			Text { id: shown  width: 10 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "shown"), 0, 0, 10, 10)
}

func TestNestedLayout(t *testing.T) {
	tr := build(t, `Window {
		Column {
			padding: 5
			Row {
				id: row
				height: 50
				padding: 2
				Text { id: a width: 20 height: 10 }
				Text { id: b width: 20 height: 10 }
			}
			Text { id: after width: 100 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "row"), 5, 5, 290, 50)
	want(t, bounds(t, tr, "a"), 2, 2, 20, 10)
	want(t, bounds(t, tr, "b"), 22, 2, 20, 10)
	want(t, bounds(t, tr, "after"), 5, 55, 100, 10)
}

func TestAutoSizeUsesMeasurer(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Text { id: a text: "hello" fontSize: 10 }
		}
	}`)
	// FakeMeasurer: width = len*fontSize*0.55 = 27.5, height = 12.
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "a"), 0, 0, 27.5, 12)
}

func TestZeroVisibleChildren(t *testing.T) {
	tr := build(t, `Window { Column { id: c } }`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "c"), 0, 0, 300, 200)
}

func TestRootFillsClientArea(t *testing.T) {
	tr := build(t, `Window { }`)
	r := New(nil).Layout(tr.Root, 640, 480)
	want(t, r, 0, 0, 640, 480)
	want(t, tr.Root.Base().Bounds(), 0, 0, 640, 480)
}

func TestGoldenTreeLayout(t *testing.T) {
	tr := build(t, `Window {
		Column {
			padding: 12
			spacing: 8
			Text   { id: title text: "Counter" fontSize: 24 }
			Button { id: inc   label: "Increase" }
			Row    { id: toolbar spacing: 6 align: center
				Text { id: left  width: 30 height: 10 }
				Text { id: right width: 30 height: 10 }
			}
		}
	}`)
	New(nil).Layout(tr.Root, 480, 320)

	type snap struct {
		ID string
		R  tree.Rect
	}
	var got []snap
	for _, n := range tr.All() {
		got = append(got, snap{n.Base().ID(), n.Base().Bounds()})
	}
	w := map[string]tree.Rect{}
	for _, s := range got {
		if s.ID != "" {
			w[s.ID] = s.R
		}
	}
	want(t, w["title"], 12, 12, 92.4, 28.8)
	want(t, w["inc"], 12, 48.8, 109.6, 40)
	want(t, w["toolbar"], 12, 96.8, 456, 10)
	want(t, w["left"], 0, 0, 30, 10)
	want(t, w["right"], 36, 0, 30, 10)
}

func BenchmarkLayout10k(b *testing.B) {
	var src string
	src = "Window { Column { spacing: 1"
	for i := 0; i < 10000; i++ {
		src += ` Text { width: 10 height: 10 }`
	}
	src += " } }"
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		b.Fatal(diag)
	}
	e := New(nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Layout(tr.Root, 1024, 768)
	}
}

// TestScrollExtentIsContentPastBox guards the wheel/drag clamp: a scrolling
// column may only move as far as its children reach past the content box.
func TestScrollExtentIsContentPastBox(t *testing.T) {
	tr := build(t, `Window {
		Column {
			id: page
			height: 100
			padding: 10
			scroll: true
			Column { id: content height: 300 }
		}
	}`)
	New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("page")
	b := n.Base()
	// Content starts at y=10 and is 300 tall, so it ends at 310; the visible
	// bottom of the content box is 100-10=90.
	if got := b.ScrollMax(); got != 220 {
		t.Fatalf("ScrollMax = %v, want 220", got)
	}
	b.SetScrollOffset(1000)
	if got := b.ScrollOffset(); got != 220 {
		t.Fatalf("clamped offset = %v, want 220", got)
	}
	b.SetScrollOffset(-5)
	if got := b.ScrollOffset(); got != 0 {
		t.Fatalf("negative offset = %v, want 0", got)
	}
}

// TestRowScrollsHorizontally checks the axis follows the container's flow.
func TestRowScrollsHorizontally(t *testing.T) {
	tr := build(t, `Window {
		Row {
			id: strip
			width: 100
			scroll: true
			Chip { label: "one" }
			Chip { label: "two" }
		}
	}`)
	New(nil).Layout(tr.Root, 400, 100)
	n, _ := tr.ByID("strip")
	b := n.Base()
	if b.ScrollAxis() != tree.AxisX {
		t.Fatalf("axis = %v, want AxisX", b.ScrollAxis())
	}
	if b.ScrollMax() <= 0 {
		t.Fatalf("ScrollMax = %v, want the chips to overflow", b.ScrollMax())
	}
}

// TestFlexShrinksForScroll guards the bounded scroll container: flex: 1 on a
// page whose content does not fit must clamp the page to the space left, or it
// grows to its content and has nothing to scroll.
func TestFlexShrinksForScroll(t *testing.T) {
	tr := build(t, `Window {
		Column {
			Text { id: head width: 10 height: 20 }
			Column {
				id: page
				flex: 1
				scroll: true
				Column { id: tall height: 500 }
			}
			Text { id: foot width: 10 height: 20 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "head"), 0, 0, 10, 20)
	want(t, bounds(t, tr, "page"), 0, 20, 300, 160)
	want(t, bounds(t, tr, "foot"), 0, 180, 10, 20)
	if n, _ := tr.ByID("page"); n.Base().ScrollMax() <= 0 {
		t.Fatal("page has nothing to scroll")
	}
}

func TestWrapStartsNewRun(t *testing.T) {
	tr := build(t, `Window {
		Wrap {
			spacing: 10
			Text { id: a width: 40 height: 10 }
			Text { id: b width: 40 height: 20 }
			Text { id: c width: 40 height: 10 }
			Text { id: d width: 40 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 100, 200)
	want(t, bounds(t, tr, "a"), 0, 0, 40, 10)
	want(t, bounds(t, tr, "b"), 50, 0, 40, 20)
	// 50+40+10 = 100, so c cannot fit and starts the second run.
	want(t, bounds(t, tr, "c"), 0, 30, 40, 10)
	want(t, bounds(t, tr, "d"), 50, 30, 40, 10)
}

func TestWrapAlignsInsideRun(t *testing.T) {
	tr := build(t, `Window {
		Wrap {
			spacing: 10
			align: center
			Text { id: a width: 40 height: 10 }
			Text { id: b width: 40 height: 20 }
		}
	}`)
	New(nil).Layout(tr.Root, 100, 200)
	// Run height is 20, so the 10dp child sits 5dp down.
	want(t, bounds(t, tr, "a"), 0, 5, 40, 10)
	want(t, bounds(t, tr, "b"), 50, 0, 40, 20)
}

func TestWrapStretchFillsRun(t *testing.T) {
	tr := build(t, `Window {
		Wrap {
			spacing: 10
			align: stretch
			Text { id: a width: 40 }
			Text { id: b width: 40 fontSize: 32 }
		}
	}`)
	New(nil).Layout(tr.Root, 100, 200)
	// The short child stretches to the run height set by the tall one.
	want(t, bounds(t, tr, "a"), 0, 0, 40, 38.4)
}

func TestWrapHugsItsContentHeight(t *testing.T) {
	tr := build(t, `Window {
		Column {
			align: start
			Wrap {
				width: 100
				spacing: 10
				Text { width: 40 height: 10 }
				Text { width: 40 height: 10 }
				Text { width: 40 height: 10 }
			}
			Text { id: below width: 10 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	// Two runs of 10dp plus the 10dp gap: the text below starts at 30.
	want(t, bounds(t, tr, "below"), 0, 30, 10, 10)
}

func TestGridColumnsAndSpacing(t *testing.T) {
	tr := build(t, `Window {
		Grid {
			columns: 3
			spacing: 10
			Text { id: a height: 20 }
			Text { id: b height: 30 }
			Text { id: c height: 10 }
			Text { id: d height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	// Cells are (300-20)/3 = 93.333 wide; row height is the tallest child.
	want(t, bounds(t, tr, "a"), 0, 0, 93.333, 20)
	want(t, bounds(t, tr, "b"), 103.333, 0, 93.333, 30)
	want(t, bounds(t, tr, "c"), 206.666, 0, 93.333, 10)
	want(t, bounds(t, tr, "d"), 0, 40, 93.333, 10)
}

func TestGridContainerChildFillsCell(t *testing.T) {
	tr := build(t, `Window {
		Column {
			align: start
			Grid {
				columns: 2
				spacing: 8
				Column { id: a padding: 4 }
			}
		}
	}`)
	New(nil).Layout(tr.Root, 208, 200)
	// The grid stretches to the column, so each cell is (208-8)/2 = 100.
	want(t, bounds(t, tr, "a"), 0, 0, 100, 8)
}

func TestResponsiveGridColumns(t *testing.T) {
	tr := build(t, `Window {
		Grid {
			width: 300
			spacing: 10
			columns: { base: 1, md: 3 }
			Text { id: a height: 10 }
			Text { id: b height: 10 }
			Text { id: c height: 10 }
		}
	}`)
	e := New(nil)

	tr.ApplyBreakpoints(500)
	e.Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "a"), 0, 0, 300, 10)
	want(t, bounds(t, tr, "b"), 0, 20, 300, 10)
	want(t, bounds(t, tr, "c"), 0, 40, 300, 10)

	tr.ApplyBreakpoints(800)
	e.Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "a"), 0, 0, 93.333, 10)
	want(t, bounds(t, tr, "b"), 103.333, 0, 93.333, 10)
	want(t, bounds(t, tr, "c"), 206.666, 0, 93.333, 10)
}

func TestPositionedLeftTop(t *testing.T) {
	tr := build(t, `Window {
		Stack {
			width: 300
			height: 200
			Rectangle { id: bg }
			Positioned { id: box left: 10 top: 20 width: 40 height: 30 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	// A plain Stack child still fills.
	want(t, bounds(t, tr, "bg"), 0, 0, 300, 200)
	want(t, bounds(t, tr, "box"), 10, 20, 40, 30)
}

func TestPositionedStretchesBetweenEdges(t *testing.T) {
	tr := build(t, `Window {
		Stack {
			width: 300
			height: 200
			Positioned {
				id: box
				left: 10
				right: 10
				top: 20
				bottom: 30
				Text { text: "x" }
			}
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "box"), 10, 20, 280, 150)
}

func TestPositionedRightBottom(t *testing.T) {
	tr := build(t, `Window {
		Stack {
			width: 300
			height: 200
			Positioned { id: box right: 15 bottom: 25 width: 50 height: 20 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "box"), 235, 155, 50, 20)
}

func TestPositionedUsesStackAlign(t *testing.T) {
	tr := build(t, `Window {
		Stack {
			width: 300
			height: 200
			align: center
			Positioned { id: box width: 40 height: 20 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "box"), 130, 90, 40, 20)
}

func TestPositionedHugsChild(t *testing.T) {
	tr := build(t, `Window {
		Stack {
			width: 300
			height: 200
			Positioned {
				id: box
				left: 12
				top: 8
				Rectangle { width: 60 height: 24 }
			}
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "box"), 12, 8, 60, 24)
}

func TestStackSizesToPositionedExtent(t *testing.T) {
	tr := build(t, `Window {
		Column {
			align: start
			Stack {
				Positioned { left: 10 top: 5 width: 40 height: 20 }
			}
			Text { id: below width: 10 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 300, 200)
	want(t, bounds(t, tr, "below"), 0, 25, 10, 10)
}
