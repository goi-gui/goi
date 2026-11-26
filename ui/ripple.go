package ui

import (
	"time"

	"github.com/goi-gui/goi/internal/tree"
)

// rippleDur is how long one press ripple lives: expansion, then fade.
const rippleDur = 0.45 // seconds

// ripple is one running press feedback. Paint reads the progress from the
// component, so a frame in between two ticks draws the same state.
type ripple struct {
	n    tree.Node
	x, y float64
	age  float64
}

// startRipple begins the press feedback at a window position.
func (i *interactor) startRipple(n tree.Node, x, y float64) {
	if n == nil {
		return
	}
	b := n.Base()
	b.RippleOn = true
	b.Ripple = 0.001
	b.RippleX, b.RippleY = x, y
	i.ripples = append(i.ripples, ripple{n: n, x: x, y: y})
	b.MarkDirty(tree.DirtyPaint)
}

// tick advances every live ripple by dt seconds and reports whether another
// frame is needed to finish them. A selection drag that sits past the field
// edge also keeps scrolling, so holding the pointer at the edge reveals the
// rest of a long value. The wait is the next caret blink, which lets the loop
// sleep instead of drawing at the display rate while a field is focused.
func (i *interactor) tick(dt float64) (bool, time.Duration) {
	scrolling := i.autoScroll(dt)
	if i.tickFling(dt) {
		scrolling = true
	}
	if i.tickSnap(dt) {
		scrolling = true
	}
	if i.tickSwitches(dt) {
		scrolling = true
	}
	if i.tickFieldLabels(dt) {
		scrolling = true
	}
	blinkChanged, blinkWait := i.tickCaretBlink(dt)
	if len(i.ripples) == 0 {
		return scrolling || blinkChanged, blinkWait
	}
	live := i.ripples[:0]
	for _, r := range i.ripples {
		b := r.n.Base()
		r.age += dt
		if b == nil || r.age >= rippleDur {
			if b != nil {
				b.RippleOn = false
				b.MarkDirty(tree.DirtyPaint)
			}
			continue
		}
		b.Ripple = r.age / rippleDur
		b.RippleX, b.RippleY = r.x, r.y
		b.MarkDirty(tree.DirtyPaint)
		live = append(live, r)
	}
	i.ripples = live
	return len(live) > 0 || scrolling || blinkChanged, blinkWait
}

// autoScrollPx is how fast a held drag scrolls a text field, in logical pixels
// per second.
const autoScrollPx = 600.0

// autoScroll keeps a selection drag moving while the pointer is outside the
// field's text box, and reports whether it moved the caret. Only the x used for
// caret mapping moves, so releasing the pointer still selects what it points at.
func (i *interactor) autoScroll(dt float64) bool {
	if !i.selecting || i.textField == nil {
		return false
	}
	n := i.focusNode(i.textField)
	if n == nil {
		return false
	}
	box := fieldTextBox(n)
	step := autoScrollPx * dt
	switch {
	case i.dragX > box.Right():
		i.dragX += step
	case i.dragX < box.X:
		i.dragX -= step
	default:
		return false
	}
	i.caretFromPoint(i.textField, i.dragX, i.dragY, true)
	return true
}
