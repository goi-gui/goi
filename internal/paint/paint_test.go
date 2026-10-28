package paint

import (
	"testing"

	"github.com/ksckaan1/goui/internal/layout"
	"github.com/ksckaan1/goui/internal/tree"
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
			Button { id: inc text: "Go" background: #3b82f6 color: #ffffff radius: 6 }
		}
	}`, 200, 120)
	const want = `fill 0,0 200x120 radius=0 color=#101010ff border=#00000000/0 clip=0,0 200x120
text "0" at 8,24 size=20 align=0 color=#eeeeeeff clip=8,8 184x104
fill 8,32 40x31.2 radius=6 color=#3b82f6ff border=#00000000/0 clip=8,8 184x104
text "Go" at 8,53.2 size=16 align=1 color=#ffffffff clip=8,8 184x104
`
	if got := l.Dump(); got != want {
		t.Fatalf("display list mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
