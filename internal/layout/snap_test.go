package layout_test

import (
	"fmt"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// snapTree lays out one scrolling column of 200dp blocks and returns its root.
func snapTree(t *testing.T, src string, w, h float64) *tree.Tree {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(nil).Layout(tr.Root, w, h)
	return tr
}

// pageBase returns the node every snap fixture names `page`.
func pageBase(t *testing.T, tr *tree.Tree) *tree.NodeBase {
	t.Helper()
	n, ok := tr.ByID("page")
	if !ok {
		t.Fatal("the fixture has no page")
	}
	return n.Base()
}

// TestSnapTargetsFollowAlignAndPadding checks the geometry: a child snaps its
// start edge to the frame, inset by scrollPadding, and center and end line up
// the other edges.
func TestSnapTargetsFollowAlignAndPadding(t *testing.T) {
	tr := snapTree(t, `Window { Column {
		id: page
		scroll: true
		scrollSnapType: mandatory
		scrollPadding: 10
		width: 200
		height: 300

		for range 3 {
			Rectangle {
				width:           200
				height:          200
				scrollSnapAlign: start
			}
		}
	} }`, 200, 300)
	got := pageBase(t, tr).SnapTargets(tree.AxisY)
	// Child tops are 0, 200 and 400; the padding pushes each line 10 in, so
	// the offsets are -10 (clamped to 0), 190 and 390. The content is 600 in a
	// 300 frame, so 390 clamps to the end at 300.
	want := []float64{0, 190, 300}
	if len(got) != len(want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("targets = %v, want %v", got, want)
		}
	}
}

// TestSnapAlignCenterAndEnd checks the other two edges a child can name.
func TestSnapAlignCenterAndEnd(t *testing.T) {
	tr := snapTree(t, `Window { Row {
		id: page
		scroll: true
		scrollSnapType: mandatory
		width: 300
		height: 100

		for range 3 {
			Rectangle {
				width:           200
				height:          100
				scrollSnapAlign: center
			}
		}
	} }`, 300, 100)
	b := pageBase(t, tr)
	// A 200-wide child centred in a 300 frame: the first child's centre is 100,
	// so its offset is 100-150 = -50, clamped to 0; the second is 300-150 = 150.
	if got := b.SnapTargets(tree.AxisX); len(got) != 3 || got[0] != 0 || got[1] != 150 {
		t.Fatalf("centre targets = %v", got)
	}
	tr = snapTree(t, `Window { Row {
		id: page
		scroll: true
		scrollSnapType: mandatory
		width: 300
		height: 100

		for range 3 {
			Rectangle {
				width:           200
				height:          100
				scrollSnapAlign: end
			}
		}
	} }`, 300, 100)
	b = pageBase(t, tr)
	// End alignment puts the child's far edge at the frame's: 200-300 = -100
	// clamps to 0, then 400-300 = 100 and 600-300 = 300.
	if got := b.SnapTargets(tree.AxisX); len(got) != 3 || got[0] != 0 || got[1] != 100 || got[2] != 300 {
		t.Fatalf("end targets = %v", got)
	}
}

// TestSnapOffsetAndNext checks the two choices the input layer makes: the
// nearest point for a settle, and the next one for a wheel notch.
func TestSnapOffsetAndNext(t *testing.T) {
	tr := snapTree(t, `Window { Column {
		id: page
		scroll: true
		scrollSnapType: mandatory
		width: 200
		height: 300

		for range 4 {
			Rectangle {
				width:           200
				height:          200
				scrollSnapAlign: start
			}
		}
	} }`, 200, 300)
	b := pageBase(t, tr)
	if got, ok := b.SnapOffset(tree.AxisY, 240); !ok || got != 200 {
		t.Fatalf("nearest to 240 = %v, %v", got, ok)
	}
	if got, ok := b.SnapNext(tree.AxisY, 0, 1); !ok || got != 200 {
		t.Fatalf("next after 0 = %v, %v", got, ok)
	}
	if got, ok := b.SnapNext(tree.AxisY, 200, -1); !ok || got != 0 {
		t.Fatalf("previous before 200 = %v, %v", got, ok)
	}
	// Past the last point the wheel stops at the end instead of running on.
	if got, _ := b.SnapNext(tree.AxisY, 500, 1); got != b.ScrollMaxFor(tree.AxisY) {
		t.Fatalf("next past the end = %v, max %v", got, b.ScrollMaxFor(tree.AxisY))
	}
}

// TestSnapProximityLeavesAFarStopAlone: proximity only takes a point the reader
// stopped near, while mandatory always does.
func TestSnapProximityLeavesAFarStopAlone(t *testing.T) {
	src := `Window { Column {
		id: page
		scroll: true
		scrollSnapType: %s
		width: 200
		height: 300

		for range 4 {
			Rectangle {
				width:           200
				height:          200
				scrollSnapAlign: start
			}
		}
	} }`
	for _, tc := range []struct {
		kind string
		off  float64
		ok   bool
	}{
		{"mandatory", 260, true},
		{"proximity", 260, true},  // 60 away from 200, inside a quarter of 300
		{"proximity", 100, false}, // 100 away, further than a quarter of the box
	} {
		tr := snapTree(t, fmt.Sprintf(src, tc.kind), 200, 300)
		got, ok := pageBase(t, tr).SnapOffset(tree.AxisY, tc.off)
		if ok != tc.ok {
			t.Fatalf("%s at %v: snapped = %v, want %v", tc.kind, tc.off, ok, tc.ok)
		}
		if ok && got != 200 {
			t.Fatalf("%s at %v: target = %v, want 200", tc.kind, tc.off, got)
		}
	}
}

// TestSnapIgnoresANonScrollingNode: the properties do nothing on an element
// that does not scroll, so a design can set them on a reusable child.
func TestSnapIgnoresANonScrollingNode(t *testing.T) {
	tr := snapTree(t, `Window { Column {
		id: page
		scrollSnapType: mandatory
		width: 200
		height: 300

		Rectangle { width: 200 height: 200 scrollSnapAlign: start }
	} }`, 200, 300)
	b := pageBase(t, tr)
	if b.Snapping() {
		t.Fatal("a node that does not scroll reports snapping")
	}
	if got := b.SnapTargets(tree.AxisY); got != nil {
		t.Fatalf("targets on a non-scrolling node = %v", got)
	}
}
