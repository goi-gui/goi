package ui

import "testing"

const pointerEventsUI = `Window {
	Stack {
		Rectangle { id: behind width: 200 height: 200 }
		Rectangle { id: overlay width: 200 height: 200 pointerEvents: none }
	}
}`

func TestPointerEventsNonePassesThrough(t *testing.T) {
	_, win, in := inputFixture(t, pointerEventsUI, 300, 220)
	behind := MustByID[*Rectangle](win, "behind")
	if got := in.hit(100, 100); got != behind.Base().Node() {
		t.Fatalf("hit = %v, want the rectangle behind the none overlay", got)
	}
}

const pointerEventsChildUI = `Window {
	Column {
		pointerEvents: none
		width: 200
		height: 200

		Button { id: child pointerEvents: auto width: 100 height: 40 label: "Go" }
		Rectangle { id: hidden width: 100 height: 40 }
	}
}`

func TestPointerEventsAutoChildOptsBackIn(t *testing.T) {
	_, win, in := inputFixture(t, pointerEventsChildUI, 300, 220)
	child := MustByID[*Button](win, "child")
	hidden := MustByID[*Rectangle](win, "hidden")
	if !child.PointerEventsEnabled() {
		t.Fatal("an auto child under a none parent should opt back in")
	}
	if hidden.PointerEventsEnabled() {
		t.Fatal("a child without its own value should inherit none")
	}
	b := absoluteRect(child)
	if got := in.hit(b.X+5, b.Y+5); got != child.Base().Node() {
		t.Fatalf("hit = %v, want the auto child", got)
	}
}
