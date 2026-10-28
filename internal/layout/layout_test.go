package layout

import (
	"testing"

	"github.com/ksckaan1/goui/internal/tree"
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
