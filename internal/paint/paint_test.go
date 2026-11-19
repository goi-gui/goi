package paint

import (
	"time"

	"fmt"
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

func list(t *testing.T, src string, w, h float64) *List {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, w, h)
	l := &List{}
	l.Build(tr, w, h)
	return l
}

// listAt builds the list with the frame clock set, which is what moves an
// animation that reads it.
func listAt(t *testing.T, src string, w, h float64, clock time.Duration) *List {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, w, h)
	l := &List{Clock: clock}
	l.Build(tr, w, h)
	return l
}

func TestOmitsInvisibleAndTransparent(t *testing.T) {
	l := list(t, `Window {
		Rectangle { id: none }
		Rectangle { id: shown width: 10 height: 10 background: #ff0000 }
		Rectangle { id: hidden width: 10 height: 10 background: #00ff00 visible: false }
	}`, 100, 100)
	if len(l.Ops) != 1 {
		t.Fatalf("ops = %d:\n%s", len(l.Ops), l.Dump())
	}
	if l.Ops[0].Color != 0xFF0000FF {
		t.Fatalf("color = %08x", l.Ops[0].Color)
	}
}

func TestAbsoluteCoordinates(t *testing.T) {
	l := list(t, `Window {
		Column {
			padding: 10
			Rectangle { id: a width: 20 height: 20 background: #ffffff }
			Rectangle { id: b width: 20 height: 20 background: #ffffff }
		}
	}`, 100, 100)
	if len(l.Ops) != 4 { // column bg? no: transparent column emits nothing; two rects
		t.Logf("%s", l.Dump())
	}
	if l.Ops[0].Rect != [4]float32{10, 10, 20, 20} {
		t.Fatalf("first rect = %v", l.Ops[0].Rect)
	}
	if l.Ops[1].Rect != [4]float32{10, 30, 20, 20} {
		t.Fatalf("second rect = %v", l.Ops[1].Rect)
	}
}

func TestNestedClip(t *testing.T) {
	l := list(t, `Window {
		Stack {
			width: 50 height: 50 background: #ff0000
			padding: 5
			Rectangle { width: 100 height: 100 background: #00ff00 }
		}
	}`, 200, 200)
	if len(l.Ops) != 2 {
		t.Fatalf("ops=%d\n%s", len(l.Ops), l.Dump())
	}
	// Child clip is the parent's content box: 40x40 at 5,5.
	if got := l.Ops[1].Clip; got != [4]float32{5, 5, 40, 40} {
		t.Fatalf("clip = %v", got)
	}
}

func TestBorderAndRadius(t *testing.T) {
	l := list(t, `Window { Rectangle { width: 10 height: 10 background: #112233 radius: 4 borderColor: #ffffff borderThickness: 2 } }`, 100, 100)
	op := l.Ops[0]
	if op.Radius[0] != 4 || op.BorderThickness != 2 || op.BorderColor != 0xFFFFFFFF {
		t.Fatalf("op = %+v", op)
	}
}

func TestGoldenDisplayList(t *testing.T) {
	l := list(t, `Window {
	id: main
	background: #101010
	Column {
		padding: 8
		Text { id: label text: "0" fontSize: 20 color: #eeeeee }
		Rectangle { id: box width: 40 height: 32 background: #3b82f6 radius: 6 }
	}
}`, 200, 120)
	const want = `fill 0,0 200x120 r=0/0/0/0 color=#101010ff border=#00000000/0 clip=0,0 200x120
text "0" at 8,26.84 size=20 w=400 align=0 color=#eeeeeeff clip=0,0 200x120
fill 8,32 40x32 r=6/6/6/6 color=#3b82f6ff border=#00000000/0 clip=0,0 200x120
`
	if got := l.Dump(); got != want {
		t.Fatalf("display list mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestScrollbarOnlyOnOverflow checks the M3 thumb: 4dp wide, 32dp minimum
// length, and absent when the content fits.
func TestScrollbarOnlyOnOverflow(t *testing.T) {
	thumb := withAlpha(m3.Light().Colors.OnSurfaceVariant, 0.4)
	overflow := list(t, `Window { Column {
		scroll: true
		autoHideScrollbar: false
		width: 100
		height: 60
		Rectangle { width: 100 height: 300 background: #ff0000 }
	} }`, 100, 100)
	found := false
	for _, op := range overflow.Ops {
		if op.Kind == Fill && op.Color == thumb {
			found = true
			if op.Rect[2] != 4 {
				t.Fatalf("thumb width = %v, want 4", op.Rect[2])
			}
			if op.Rect[3] != 32 {
				t.Fatalf("thumb height = %v, want the 32 minimum", op.Rect[3])
			}
		}
	}
	if !found {
		t.Fatalf("no thumb:\n%s", overflow.Dump())
	}

	fits := list(t, `Window { Column {
		scroll: true
		autoHideScrollbar: false
		width: 100
		height: 60
		Rectangle { width: 100 height: 40 background: #ff0000 }
	} }`, 100, 100)
	for _, op := range fits.Ops {
		if op.Kind == Fill && op.Color == thumb {
			t.Fatal("thumb drawn although the content fits")
		}
	}
}

// TestScrollbarVisibility covers the two switches: hide removes the bar even
// though the content overflows, and auto-hide (the default) keeps it down until
// the pointer or a scroll asks for it.
func TestScrollbarVisibility(t *testing.T) {
	thumb := withAlpha(m3.Light().Colors.OnSurfaceVariant, 0.4)
	src := `Window { Column {
		scroll: true
		%s
		width: 100
		height: 60
		Rectangle { width: 100 height: 300 background: #ff0000 }
	} }`
	hasThumb := func(s string) bool {
		l := list(t, s, 100, 100)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Color == thumb {
				return true
			}
		}
		return false
	}
	if hasThumb(fmt.Sprintf(src, "hideScrollbar: true")) {
		t.Fatal("bar drawn although hideScrollbar is true")
	}
	if hasThumb(fmt.Sprintf(src, "")) {
		t.Fatal("auto-hiding bar drawn with no pointer and no scroll")
	}
	if !hasThumb(fmt.Sprintf(src, "autoHideScrollbar: false")) {
		t.Fatal("bar missing although auto-hide is off")
	}
}

// TestOpacityFadesSubtree checks that an opacity multiplies the alpha of the
// node, its children and their borders, and clamps to the 0..1 range.
func TestOpacityFadesSubtree(t *testing.T) {
	l := list(t, `Window {
		Column {
			opacity: 0.5
			Rectangle { width: 10 height: 10 background: #ff0000 }
			Rectangle { width: 10 height: 10 background: #00ff00 borderColor: #0000ff borderThickness: 2 }
		}
	}`, 100, 100)
	var fills int
	for _, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		fills++
		if op.Color&0xFF != 0x80 && op.Color != 0 {
			t.Fatalf("fill alpha = %#x, want half", op.Color&0xFF)
		}
		if op.BorderThickness > 0 && op.BorderColor&0xFF != 0x80 {
			t.Fatalf("border alpha = %#x, want half", op.BorderColor&0xFF)
		}
	}
	if fills != 2 {
		t.Fatalf("fills = %d, want both rectangles", fills)
	}

	// The clamp keeps a wild value from painting nothing visible by accident:
	// above 1 is opaque, below 0 is invisible.
	high := list(t, `Window { Rectangle { width: 10 height: 10 background: #ff0000 opacity: 4 } }`, 100, 100)
	if high.Ops[0].Color != 0xFF0000FF {
		t.Fatalf("opacity 4 = %#x, want opaque", high.Ops[0].Color)
	}
	low := list(t, `Window { Rectangle { width: 10 height: 10 background: #ff0000 opacity: -1 } }`, 100, 100)
	if low.Ops[0].Color&0xFF != 0 {
		t.Fatalf("opacity -1 = %#x, want fully transparent", low.Ops[0].Color)
	}
}

// TestScrollShadowFollowsOffset checks the per-edge rule: the top band appears
// once the content has moved down, the bottom one stays until the end.
func TestScrollShadowFollowsOffset(t *testing.T) {
	src := `Window { Column {
		id: page
		scroll: true
		scrollShadow: true
		scrollShadowColor: #000000
		width: 100
		height: 100
		Rectangle { width: 100 height: 300 background: #ff0000 }
	} }`
	bands := func(offset float64) (top, bottom bool) {
		tr, diag := tree.BuildSource([]byte(src), nil)
		if !diag.Empty() {
			t.Fatalf("%v", diag)
		}
		layout.New(nil).Layout(tr.Root, 100, 100)
		page, _ := tr.ByID("page")
		page.Base().SetScrollOffset(offset)
		l := &List{}
		l.Build(tr, 100, 100)
		for _, op := range l.Ops {
			if op.Kind != Fill || op.Shape != Gradient {
				continue
			}
			if op.Rect[1] == 0 {
				top = true
			} else {
				bottom = true
			}
		}
		return
	}
	if top, bottom := bands(0); top || !bottom {
		t.Fatalf("at the top: top=%v bottom=%v, want only the bottom band", top, bottom)
	}
	if top, bottom := bands(100); !top || !bottom {
		t.Fatalf("in the middle: top=%v bottom=%v, want both", top, bottom)
	}
	if top, bottom := bands(200); !top || bottom {
		t.Fatalf("at the end: top=%v bottom=%v, want only the top band", top, bottom)
	}

	// Off by default.
	plain := list(t, `Window { Column {
		scroll: true
		width: 100 height: 100
		Rectangle { width: 100 height: 300 }
	} }`, 100, 100)
	for _, op := range plain.Ops {
		if op.Shape == Gradient {
			t.Fatal("scroll shadow painted without scrollShadow")
		}
	}
}

// TestScrollbarWidth checks that the property resizes the bar and that a
// non-positive value falls back to the M3 default.
func TestScrollbarWidth(t *testing.T) {
	thumbW := func(prop string) float64 {
		l := list(t, fmt.Sprintf(`Window { Column {
			scroll: true
			autoHideScrollbar: false
			%s
			width: 100
			height: 60
			Rectangle { width: 100 height: 300 background: #ff0000 }
		} }`, prop), 100, 100)
		col := withAlpha(m3.Light().Colors.OnSurfaceVariant, 0.4)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Color == col {
				return float64(op.Rect[2])
			}
		}
		t.Fatalf("no thumb for %q:\n%s", prop, l.Dump())
		return 0
	}
	if w := thumbW("scrollbarWidth: 10"); w != 10 {
		t.Fatalf("thumb width = %v, want 10", w)
	}
	if w := thumbW("scrollbarWidth: 0"); w != tree.ScrollbarThickness {
		t.Fatalf("thumb width = %v, want the default %v", w, tree.ScrollbarThickness)
	}
}

// TestScrollbarColor checks the override and the themed default: a declared
// colour paints exactly as written, and leaving it out keeps the translucent
// onSurfaceVariant thumb.
func TestScrollbarColor(t *testing.T) {
	src := `Window { Column {
		scroll: true
		autoHideScrollbar: false
		%s
		width: 100
		height: 60
		Rectangle { width: 100 height: 300 background: #ff0000 }
	} }`
	thumbColor := func(prop string) uint32 {
		l := list(t, fmt.Sprintf(src, prop), 100, 100)
		def := withAlpha(m3.Light().Colors.OnSurfaceVariant, 0.4)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Color != 0 && op.Color != 0xFF0000FF && (op.Color == def || op.Color == 0x00FF00FF) {
				return op.Color
			}
		}
		t.Fatalf("no thumb for %q:\n%s", prop, l.Dump())
		return 0
	}
	if got := thumbColor("scrollbarColor: #00ff00"); got != 0x00FF00FF {
		t.Fatalf("thumb colour = %#x, want #00ff00ff", got)
	}
	if got, want := thumbColor(""), withAlpha(m3.Light().Colors.OnSurfaceVariant, 0.4); got != want {
		t.Fatalf("thumb colour = %#x, want default %#x", got, want)
	}
}

// TestScrollChromeIgnoresPadding guards the frame rule: padding spaces only
// the moving content, so the bar and the edge shadows sit against the frame
// instead of floating inside the padding.
func TestScrollChromeIgnoresPadding(t *testing.T) {
	l := list(t, `Window { Column {
		scroll: true
		scrollShadow: true
		autoHideScrollbar: false
		padding: 10
		width: 100
		height: 60
		Rectangle { width: 80 height: 300 background: #ff0000 }
	} }`, 100, 100)
	var thumb *Op
	var bottom *Op
	for i := range l.Ops {
		op := &l.Ops[i]
		if op.Kind != Fill {
			continue
		}
		switch {
		case op.Shape == Gradient && op.Rect[1] > 0:
			bottom = op
		case op.Color == withAlpha(m3.Light().Colors.OnSurfaceVariant, 0.4):
			thumb = op
		}
	}
	if thumb == nil {
		t.Fatalf("no thumb:\n%s", l.Dump())
	}
	if want := float32(100 - tree.ScrollbarThickness - tree.ScrollbarInset); thumb.Rect[0] != want {
		t.Fatalf("thumb x = %v, want %v (padding ignored)", thumb.Rect[0], want)
	}
	if bottom == nil {
		t.Fatalf("no bottom shadow:\n%s", l.Dump())
	}
	if bottom.Rect[0] != 0 || bottom.Rect[2] != 100 {
		t.Fatalf("shadow x/w = %v/%v, want 0/100 (padding ignored)", bottom.Rect[0], bottom.Rect[2])
	}
}

// TestScrollClipIgnoresPaddingOnEveryEdge checks that no edge of a padded
// scroller keeps a permanent band: content clips to the frame, so the 10dp only
// shows where the scroll boundary is (above the first child, below the last).
func TestScrollClipIgnoresPaddingOnEveryEdge(t *testing.T) {
	l := list(t, `Window { Column {
		id: page
		scroll: true
		padding: 10
		width: 100
		height: 100
		Rectangle { id: body width: 80 height: 300 background: #ff0000 }
	} }`, 100, 100)
	var body Op
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == 0xFF0000FF {
			body = op
			break
		}
	}
	if body.Rect[0] == 0 {
		t.Fatalf("no body op:\n%s", l.Dump())
	}
	// Layout still places the body after the left and top padding.
	if body.Rect[0] != 10 || body.Rect[1] != 10 {
		t.Fatalf("body at %v,%v, want the 10 padding start", body.Rect[0], body.Rect[1])
	}
	// The clip is the frame on all four edges, so a scrolled page slides under
	// the start padding instead of leaving a fixed gutter there.
	if body.Clip[1] != 0 || body.Clip[1]+body.Clip[3] != 100 {
		t.Fatalf("clip y = %v..%v, want the frame 0..100", body.Clip[1], body.Clip[1]+body.Clip[3])
	}
	if body.Clip[0] != 0 || body.Clip[0]+body.Clip[2] != 100 {
		t.Fatalf("clip x = %v..%v, want the frame 0..100", body.Clip[0], body.Clip[0]+body.Clip[2])
	}

	// Scrolled down, the body slides under the top padding; no fixed 10dp line
	// clips it away.
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		id: page
		scroll: true
		padding: 10
		width: 100
		height: 100
		Rectangle { id: body width: 80 height: 300 background: #ff0000 }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 100, 100)
	page, _ := tr.ByID("page")
	page.Base().SetScrollOffset(20)
	scrolled := &List{}
	scrolled.Build(tr, 100, 100)
	var top float32 = 1000
	for _, op := range scrolled.Ops {
		if op.Kind == Fill && op.Color == 0xFF0000FF && op.Rect[1] < top {
			top = op.Rect[1]
		}
	}
	if top > 0 {
		t.Fatalf("scrolled body starts at y=%v, want it under the frame top", top)
	}
}

// TestZIndexOrdersSiblings checks CSS stacking: a higher zIndex paints later,
// so it lands on top, and equal values keep document order. Layout is untouched.
func TestZIndexOrdersSiblings(t *testing.T) {
	src := `Window { Stack {
		width: 100
		height: 100
		Rectangle { id: red width: 100 height: 100 background: #ff0000 }
		Rectangle { id: green width: 100 height: 100 background: #00ff00 %s }
	} }`
	order := func(prop string) []uint32 {
		l := list(t, fmt.Sprintf(src, prop), 100, 100)
		var out []uint32
		for _, op := range l.Ops {
			if op.Kind == Fill && (op.Color == 0xFF0000FF || op.Color == 0x00FF00FF) {
				out = append(out, op.Color)
			}
		}
		if len(out) != 2 {
			t.Fatalf("ops = %v:\n%s", out, l.Dump())
		}
		return out
	}
	// Without zIndex the later sibling wins, as it always did.
	if got := order(""); got[0] != 0xFF0000FF || got[1] != 0x00FF00FF {
		t.Fatalf("default order = %#v, want red then green", got)
	}
	// A negative value sends it behind the earlier sibling.
	if got := order("zIndex: -1"); got[0] != 0x00FF00FF || got[1] != 0xFF0000FF {
		t.Fatalf("zIndex -1 order = %#v, want green then red", got)
	}
	// A positive value keeps it on top, which it already was, so the tie holds
	// document order rather than reversing it.
	if got := order("zIndex: 1"); got[0] != 0xFF0000FF || got[1] != 0x00FF00FF {
		t.Fatalf("zIndex 1 order = %#v, want red then green", got)
	}

	// Layout ignores it: both rectangles still fill the stack.
	l := list(t, fmt.Sprintf(src, "zIndex: -5"), 100, 100)
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == 0x00FF00FF && op.Rect[3] != 100 {
			t.Fatalf("height changed with zIndex: %v", op.Rect)
		}
	}
}

// TestHorizontalScrollIgnoresPadding is the same rule on a Row: the left
// padding is the gap before the first chip and scrolls away, not a stuck inset.
func TestHorizontalScrollIgnoresPadding(t *testing.T) {
	src := `Window { Row {
		id: strip
		scroll: true
		padding: 10
		width: 100
		height: 40
		Rectangle { id: chip width: 300 height: 20 background: #ff0000 }
	} }`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 100, 100)
	strip, _ := tr.ByID("strip")
	left := func() float32 {
		l := &List{}
		l.Build(tr, 100, 100)
		for _, op := range l.Ops {
			if op.Kind == Fill && op.Color == 0xFF0000FF {
				return op.Rect[0]
			}
		}
		t.Fatalf("no chip:\n%s", l.Dump())
		return 0
	}
	if got := left(); got != 10 {
		t.Fatalf("chip x at rest = %v, want the 10 padding start", got)
	}
	strip.Base().SetScrollOffset(20)
	if got := left(); got != -10 {
		t.Fatalf("chip x scrolled = %v, want it under the left padding", got)
	}
}

// TestWrapCacheInvalidates guards reusing one List across frames, which is what
// the app does: a changed text or column must re-wrap instead of drawing the
// cached lines.
func TestWrapCacheInvalidates(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Column {
		Text { id: label text: "one two three four" wrap: true width: 40 }
	} }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	n, _ := tr.ByID("label")
	label := n.(*tree.Text)
	l := &List{}
	lineCount := func() int {
		l.Build(tr, 400, 400)
		n := 0
		for _, op := range l.Ops {
			if op.Kind == Text {
				n++
			}
		}
		return n
	}
	before := lineCount()
	if before < 2 {
		t.Fatalf("lines = %d, want the text wrapped", before)
	}
	if again := lineCount(); again != before {
		t.Fatalf("repaint drew %d lines, want %d", again, before)
	}

	label.Text.Set("one two three four five six seven eight")
	layout.New(nil).Layout(tr.Root, 400, 400)
	if after := lineCount(); after <= before {
		t.Fatalf("lines after a text change = %d, want more than %d", after, before)
	}

	// A wider column re-wraps too: set the box through the node's own property
	// cell, which is what a design's `width` writes.
	if ok := label.Base().SetPropLiteral("width", &syntax.Number{Val: 200}); !ok {
		t.Fatal("width did not apply")
	}
	layout.New(nil).Layout(tr.Root, 400, 400)
	if narrow := lineCount(); narrow >= before {
		t.Fatalf("lines at a wider column = %d, want fewer than %d", narrow, before)
	}
}

// TestRotateTurnsSubtree checks that `rotate` marks every op a subtree emits
// with the same angle and the node's centre as pivot, which is what the shader
// turns about.
func TestRotateTurnsSubtree(t *testing.T) {
	const src = `Window { Column {
		Column {
			id:     box
			width:  200
			height: 100
			rotate: 45deg
			Text { text: "turned" }
		}
		Text { text: "still" }
	} }`
	l := list(t, src, 500, 400)
	turned := 0
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "still" {
			if op.Rotate != 0 {
				t.Fatalf("a sibling outside the rotated box turned: %+v", op)
			}
			continue
		}
		if op.Rotate != 0 {
			turned++
			if math.Abs(float64(op.Rotate)-math.Pi/4) > 1e-6 {
				t.Fatalf("angle = %v, want pi/4", op.Rotate)
			}
		}
	}
	if turned == 0 {
		t.Fatalf("nothing turned:\n%s", l.Dump())
	}
}

// TestRotateComposesNested checks that an inner turn survives an outer one and
// its pivot moves with the outer frame, which is what a rigid transform does.
func TestRotateComposesNested(t *testing.T) {
	const src = `Window { Column {
		Column {
			width:  300
			height: 300
			rotate: 90deg
			Column {
				width:  100
				height: 100
				rotate: 45deg
				Rectangle { width: 100 height: 100 background: "#ff0000ff" }
			}
		}
	} }`
	l := list(t, src, 500, 400)
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == 0xFF0000FF {
			if op.Rotate == 0 {
				continue
			}
			if math.Abs(float64(op.Rotate)-3*math.Pi/4) > 1e-6 {
				t.Fatalf("nested angle = %v, want 135 degrees", op.Rotate)
			}
			return
		}
	}
	t.Fatalf("no rectangle op:\n%s", l.Dump())
}

// TestSkeletonDrawsABaseAndASweep: the placeholder is one block plus the
// highlight band, and only the banded form keeps the frame loop awake.
func TestSkeletonDrawsABaseAndASweep(t *testing.T) {
	const win = 400.0
	l := listAt(t, `Window {
		Skeleton { width: 200 height: 20 radius: 10 }
	}`, win, 200, 500*time.Millisecond)
	if len(l.Ops) != 2 {
		t.Fatalf("ops = %d, want a base and a sweep:\n%s", len(l.Ops), l.Dump())
	}
	if l.Ops[1].Shape != CSSGradient || l.Ops[1].Gradient == nil {
		t.Fatalf("the sweep is %v, want a CSS gradient", l.Ops[1].Shape)
	}
	if !l.Animating {
		t.Fatal("an animated skeleton did not ask for the next frame")
	}
	still := listAt(t, `Window {
		Skeleton { width: 200 height: 20 animated: false }
	}`, win, 200, 500*time.Millisecond)
	if len(still.Ops) != 1 {
		t.Fatalf("a still skeleton drew %d ops, want only its block", len(still.Ops))
	}
	if still.Animating {
		t.Fatal("a still skeleton asked for the next frame")
	}
}

// TestSplitButtonTrailingIcon: the menu segment draws the icon the design named,
// and keeps the M3 arrow when it names none.
func TestSplitButtonTrailingIcon(t *testing.T) {
	icons := func(src string) []string {
		l := list(t, src, 300, 200)
		var out []string
		for _, op := range l.Ops {
			if op.Kind == Text && op.Icon {
				out = append(out, op.Text)
			}
		}
		return out
	}
	got := icons(`Window { SplitButton { label: "Split" width: 200 height: 40 trailingIcon: arrow_upward } }`)
	if len(got) != 1 || got[0] != "arrow_upward" {
		t.Fatalf("icons = %v, want the trailing arrow_upward", got)
	}
	got = icons(`Window { SplitButton { label: "Split" width: 200 height: 40 } }`)
	if len(got) != 1 || got[0] != "arrow_drop_down" {
		t.Fatalf("icons = %v, want the default arrow_drop_down", got)
	}
}

// TestScaleStretchesSubtree checks the CSS shapes of `scale`: one value is both
// axes, a percentage is its factor, two values split x and y, and the whole
// subtree scales about the node's centre while a sibling stays put.
func TestScaleStretchesSubtree(t *testing.T) {
	const src = `Window { Column {
		Column {
			id:     box
			width:  200
			height: 100
			scale:  2 50%
			Text { text: "stretched" }
		}
		Text { text: "still" }
	} }`
	l := list(t, src, 500, 400)
	scaled := 0
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "still" {
			if op.Scale[2] != 0 {
				t.Fatalf("a sibling outside the scaled box scaled: %+v", op)
			}
			continue
		}
		if op.Scale[2] == 0 {
			continue
		}
		scaled++
		if op.Scale[0] != 2 || op.Scale[1] != 0.5 {
			t.Fatalf("scale = %v, %v; want 2, 0.5", op.Scale[0], op.Scale[1])
		}
	}
	if scaled == 0 {
		t.Fatalf("nothing scaled:\n%s", l.Dump())
	}

	// The uniform forms, and `none` leaving every op alone.
	for _, tc := range []struct {
		value string
		x, y  float32
	}{
		{"2", 2, 2},
		{"50%", 0.5, 0.5},
		{"none", 1, 1},
	} {
		l := list(t, `Window { Column { width: 100 height: 100 scale: `+tc.value+` } }`, 200, 200)
		for _, op := range l.Ops {
			if op.Kind != Fill {
				continue
			}
			if tc.value == "none" {
				if op.Scale[2] != 0 {
					t.Fatalf("none scaled an op: %+v", op)
				}
				continue
			}
			if op.Scale[0] != tc.x || op.Scale[1] != tc.y {
				t.Fatalf("scale %q = %v, %v; want %v, %v", tc.value, op.Scale[0], op.Scale[1], tc.x, tc.y)
			}
		}
	}
}

// TestScaleComposesThroughThePivot: two scaled boxes multiply their factors and
// keep one pivot, which is what the shader needs to draw the inner box in the
// outer box's frame.
func TestScaleComposesThroughThePivot(t *testing.T) {
	l := list(t, `Window {
		Column {
			id:     outer
			width:  200
			height: 200
			scale:  2
			Column {
				width:  50
				height: 50
				scale:  0.5
				Rectangle { width: 50 height: 50 background: #ff0000 }
			}
		}
	}`, 400, 400)
	for _, op := range l.Ops {
		if op.Kind != Fill || op.Color != 0xFF0000FF {
			continue
		}
		if op.Scale[2] == 0 {
			t.Fatal("the nested box did not scale")
		}
		if op.Scale[0] != 1 || op.Scale[1] != 1 {
			t.Fatalf("nested factors = %v, %v; want 1, 1", op.Scale[0], op.Scale[1])
		}
		return
	}
	t.Fatal("the rectangle did not paint")
}

// TestUnscaledComponentsStayUnscaled is the regression the zero value guards: a
// component is built from a zero NodeBase, so a scale that treated 0, 0 as "off"
// shrank every component to nothing while plain elements kept drawing.
func TestUnscaledComponentsStayUnscaled(t *testing.T) {
	l := list(t, `Window {
		Column {
			Button { label: "B" width: 120 height: 40 }
			Card { label: "C" width: 120 height: 40 }
			Rectangle { width: 120 height: 40 background: #ff0000 }
			Text { text: "scaled" scale: 2 }
		}
	}`, 300, 400)
	scaled := 0
	for _, op := range l.Ops {
		if op.Scale[2] == 0 {
			continue
		}
		scaled++
		if op.Kind == Text && op.Text == "scaled" {
			if op.Scale[0] != 2 || op.Scale[1] != 2 {
				t.Fatalf("the scaled text = %v, %v; want 2, 2", op.Scale[0], op.Scale[1])
			}
			continue
		}
		t.Fatalf("an element nobody scaled is scaled: %+v", op)
	}
	if scaled != 1 {
		t.Fatalf("scaled ops = %d, want only the text", scaled)
	}
}

// TestListItemColours checks the four colour properties: each tints its own
// part and falls back to the M3 role when unset.
func TestListItemColours(t *testing.T) {
	l := list(t, `Window {
		ListItem {
			label:            "Label"
			supporting:       "Supporting"
			leadingIcon:      home
			trailingIcon:     star
			leadingIconColor:  #112233
			trailingIconColor: #223344
			labelColor:        #334455
			supportingColor:   #445566
		}
	} `, 400, 200)
	want := map[string]uint32{
		"home":       0x112233FF,
		"star":       0x223344FF,
		"Label":      0x334455FF,
		"Supporting": 0x445566FF,
	}
	seen := 0
	for _, op := range l.Ops {
		if op.Kind != Text {
			continue
		}
		c, ok := want[op.Text]
		if !ok {
			continue
		}
		seen++
		if op.Color != c {
			t.Fatalf("%q colour = %08x, want %08x", op.Text, op.Color, c)
		}
	}
	if seen != len(want) {
		t.Fatalf("tinted %d of %d parts:\n%s", seen, len(want), l.Dump())
	}
}

// TestExtendedFABHugsItsLabel is the width/alignment rule: 20dp at each end,
// the icon and a 12dp gap when there is one, and the label between them. The
// metrics and the paint have to agree or the pill grows a gap at its end.
func TestExtendedFABHugsItsLabel(t *testing.T) {
	find := func(l *List, text string, icon bool) *Op {
		for i := range l.Ops {
			op := &l.Ops[i]
			if op.Kind == Text && op.Text == text && op.Icon == icon {
				return op
			}
		}
		return nil
	}
	pill := func(l *List) *Op {
		col := l.Theme.Colors.PrimaryContainer
		for i := range l.Ops {
			op := &l.Ops[i]
			if op.Kind == Fill && op.Color == col && op.Shape == RoundedRect {
				return op
			}
		}
		return nil
	}

	l := list(t, `Window { FAB { icon: edit label: "Compose" } }`, 300, 120)
	box, label, icon := pill(l), find(l, "Compose", false), find(l, "edit", true)
	if box == nil || label == nil || icon == nil {
		t.Fatalf("missing ops:\n%s", l.Dump())
	}
	rx, rw := float64(box.Rect[0]), float64(box.Rect[2])
	if got := float64(icon.Rect[0]); got != rx+20 {
		t.Fatalf("icon left = %v, want %v", got, rx+20)
	}
	if got := float64(label.TextX); got != rx+56 {
		t.Fatalf("label left = %v, want %v", got, rx+56)
	}
	if got, want := float64(label.TextX)+float64(label.BoxW), rx+rw-20; math.Abs(got-want) > 0.01 {
		t.Fatalf("label right = %v, want %v", got, want)
	}
	if want := 20 + 24 + 12 + float64(label.BoxW) + 20; math.Abs(rw-want) > 0.01 {
		t.Fatalf("pill width = %v, want %v (20+icon+gap+label+20)", rw, want)
	}

	// A label with no icon drops the icon's width and gap, not just the glyph.
	l = list(t, `Window { FAB { label: "Compose" } }`, 300, 120)
	box, label = pill(l), find(l, "Compose", false)
	if box == nil || label == nil {
		t.Fatalf("missing ops:\n%s", l.Dump())
	}
	rx, rw = float64(box.Rect[0]), float64(box.Rect[2])
	if got := float64(label.TextX); got != rx+20 {
		t.Fatalf("label left = %v, want %v", got, rx+20)
	}
	if want := 20 + float64(label.BoxW) + 20; math.Abs(rw-want) > 0.01 {
		t.Fatalf("pill width = %v, want %v", rw, want)
	}
}

// TestIconSizedAndTinted is the whole contract of a standalone glyph: it fills
// the shorter side of its box and stays centred, takes the M3 icon role by
// default, and follows a named colour.
func TestIconSizedAndTinted(t *testing.T) {
	glyph := func(l *List) *Op {
		for i := range l.Ops {
			if l.Ops[i].Kind == Text && l.Ops[i].Icon {
				return &l.Ops[i]
			}
		}
		return nil
	}

	l := list(t, `Window { Column { Icon { icon: home } } }`, 200, 100)
	op := glyph(l)
	if op == nil || op.Text != "home" {
		t.Fatalf("no home glyph:\n%s", l.Dump())
	}
	if op.FontSize != 24 || op.Rect[2] != 24 || op.Rect[3] != 24 {
		t.Fatalf("default glyph = %v in %v, want 24dp", op.FontSize, op.Rect)
	}
	if want := l.Theme.Colors.OnSurfaceVariant; op.Color != want {
		t.Fatalf("default colour = %08x, want onSurfaceVariant %08x", op.Color, want)
	}

	// A square `size`, and a colour of its own. A role stays a name until the
	// theme runs, so the painter test uses a literal.
	l = list(t, `Window { Column { Icon { icon: star size: 40 color: #ff0000 } } }`, 200, 100)
	op = glyph(l)
	if op == nil || op.FontSize != 40 || op.Rect[2] != 40 || op.Rect[3] != 40 {
		t.Fatalf("sized glyph = %+v", op)
	}
	if op.Color != 0xFF0000FF {
		t.Fatalf("colour = %08x, want #ff0000", op.Color)
	}

	// A box that is not square uses the shorter side and centres the glyph.
	l = list(t, `Window { Column { Icon { icon: star width: 80 height: 40 color: #ff0000 } } }`, 200, 100)
	op = glyph(l)
	if op == nil || op.Rect[2] != 40 || op.Rect[3] != 40 {
		t.Fatalf("non-square glyph = %+v", op)
	}
	if op.Rect[0] != 20 {
		t.Fatalf("glyph left = %v, want 20 (centred in an 80dp box)", op.Rect[0])
	}
}

// TestIconButtonIconColor tints only the glyph and leaves the variant's
// container and state layer to the theme.
func TestIconButtonIconColor(t *testing.T) {
	l := list(t, `Window {
		Column {
			IconButton { icon: favorite iconColor: #112233 }
		}
	}`, 120, 120)
	found := false
	for _, op := range l.Ops {
		if op.Kind != Text || !op.Icon {
			continue
		}
		found = true
		if op.Color != 0x112233FF {
			t.Fatalf("icon colour = %08x, want 112233ff", op.Color)
		}
	}
	if !found {
		t.Fatal("the icon did not paint")
	}
}

// TestMissingBitmapAsksForOneFrame: an Image that lost its bitmap should make
// the list ask for another frame, where the loader retries. A Go-owned or
// given-up image must not keep the frame loop spinning.
func TestMissingBitmapAsksForOneFrame(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window { Image { id: pic src: "goi://pic.png" width: 10 height: 10 } }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 20, 20)
	n, _ := tr.ByID("pic")
	im := n.(*tree.Image)
	build := func() bool {
		l := &List{}
		l.Build(tr, 20, 20)
		return l.Animating
	}
	if !build() {
		t.Fatal("a lost bitmap did not ask for a retry frame")
	}
	im.NoRetry = true
	if build() {
		t.Fatal("a given-up image kept asking for frames")
	}
	im.NoRetry = false
	im.SetImage(nil)
	if build() {
		t.Fatal("a Go-cleared image kept asking for frames")
	}
	im.NoFile = false
	im.Src.Set("")
	if build() {
		t.Fatal("an image with no src kept asking for frames")
	}
}
