package layout

import "testing"

// TestSidePaddingOverridesPadding checks the precedence rule: a side value wins
// over padding for that edge and leaves the others alone.
func TestSidePaddingOverridesPadding(t *testing.T) {
	tr := build(t, `Window {
		Column {
			id: box
			padding: 10
			paddingLeft: 4
			paddingRight: 20
			Column { id: kid height: 10 width: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 200, 200)
	b := bounds(t, tr, "kid")
	if b.X != 4 {
		t.Errorf("child x = %v, want the left padding 4", b.X)
	}
	if b.Y != 10 {
		t.Errorf("child y = %v, want the inherited top padding 10", b.Y)
	}
}

// TestSideMarginPlacesTheBox checks the same fallback for margin, including the
// space it takes on both sides of a row.
func TestSideMarginPlacesTheBox(t *testing.T) {
	tr := build(t, `Window {
		Row {
			spacing: 6
			Rectangle { id: a width: 10 height: 10 marginLeft: 30 marginTop: 5 }
			Rectangle { id: b width: 10 height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 200, 200)
	a := bounds(t, tr, "a")
	b := bounds(t, tr, "b")
	if a.X != 30 || a.Y != 5 {
		t.Errorf("a at %v,%v, want 30,5", a.X, a.Y)
	}
	// The second box follows a's right edge plus spacing; a's margin only
	// shifted it, it does not also add a trailing gap here.
	if b.X != 30+10+6 {
		t.Errorf("b x = %v, want %v", b.X, 30+10+6)
	}
}

// TestGapIsSpacingAlias keeps the two names in step, whichever a file writes.
func TestGapIsSpacingAlias(t *testing.T) {
	tr := build(t, `Window {
		Column {
			gap: 12
			Rectangle { height: 10 }
			Rectangle { id: second height: 10 }
		}
	}`)
	New(nil).Layout(tr.Root, 200, 200)
	if got := bounds(t, tr, "second").Y; got != 22 {
		t.Fatalf("second child y = %v, want 22 (gap 12)", got)
	}
}
