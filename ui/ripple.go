package ui

import "github.com/ksckaan1/goui/internal/tree"

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
	c := comp(n)
	if c == nil {
		return
	}
	c.RippleOn = true
	c.Ripple = 0.001
	c.RippleX, c.RippleY = x, y
	i.ripples = append(i.ripples, ripple{n: n, x: x, y: y})
	c.Base().MarkDirty(tree.DirtyPaint)
}

// tick advances every live ripple by dt seconds and reports whether another
// frame is needed to finish them. A selection drag that sits past the field
// edge also keeps scrolling, so holding the pointer at the edge reveals the
// rest of a long value.
func (i *interactor) tick(dt float64) bool {
	scrolling := i.autoScroll(dt)
	if i.tickFling(dt) {
		scrolling = true
	}
	if len(i.ripples) == 0 {
		return scrolling
	}
	live := i.ripples[:0]
	for _, r := range i.ripples {
		c := comp(r.n)
		r.age += dt
		if c == nil || r.age >= rippleDur {
			if c != nil {
				c.RippleOn = false
				c.Base().MarkDirty(tree.DirtyPaint)
			}
			continue
		}
		c.Ripple = r.age / rippleDur
		c.RippleX, c.RippleY = r.x, r.y
		c.Base().MarkDirty(tree.DirtyPaint)
		live = append(live, r)
	}
	i.ripples = live
	return len(live) > 0 || scrolling
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
	i.caretFromPoint(i.textField, i.dragX, true)
	return true
}
