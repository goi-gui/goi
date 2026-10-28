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
// frame is needed to finish them.
func (i *interactor) tick(dt float64) bool {
	if len(i.ripples) == 0 {
		return false
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
	return len(live) > 0
}
