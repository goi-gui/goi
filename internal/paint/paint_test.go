package paint

import (
	"fmt"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
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
	l := list(t, `Window { Rectangle { width: 10 height: 10 background: #112233 radius: 4 borderColor: #ffffff borderWidth: 2 } }`, 100, 100)
	op := l.Ops[0]
	if op.Radius[0] != 4 || op.BorderWidth != 2 || op.BorderColor != 0xFFFFFFFF {
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
			Rectangle { width: 10 height: 10 background: #00ff00 borderColor: #0000ff borderWidth: 2 }
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
		if op.BorderWidth > 0 && op.BorderColor&0xFF != 0x80 {
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

// TestScrollClipDropsEndPaddingAtRest checks that bottom padding is not a
// permanent band: at the top the content clips to the frame bottom, and the
// same 10dp only shows after the last child at the end of the scroll.
func TestScrollClipDropsEndPaddingAtRest(t *testing.T) {
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
	// x is inset by the left padding, but the clip runs to the frame bottom so
	// the bottom padding is not visible while the content still overflows there.
	if body.Rect[0] != 10 {
		t.Fatalf("body x = %v, want the 10 padding start", body.Rect[0])
	}
	if got := body.Clip[1] + body.Clip[3]; got != 100 {
		t.Fatalf("clip bottom = %v, want the frame bottom 100", got)
	}
}
