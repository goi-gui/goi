package ui

import (
	"math"

	"github.com/goi-gui/goi/internal/tree"
)

// switchTau shapes the Switch handle's travel: the position eases toward the
// target every frame rather than running from a start time, so flipping the
// switch mid-travel turns around instead of restarting. 45 ms settles in about
// 150 ms at 60 Hz, which is the M3 switch duration.
const switchTau = 0.045 // seconds

// switchSettled is how close the handle must be to snap onto the target, so an
// animation always finishes and the loop can sleep.
const switchSettled = 0.002

// tickSwitches advances every Switch handle toward its checked state and
// reports whether one is still moving. It runs before the frame check, so the
// travel keeps asking for frames until it lands.
func (i *interactor) tickSwitches(dt float64) bool {
	if i.tree == nil || dt <= 0 {
		return false
	}
	moving := false
	i.tree.Walk(func(n tree.Node) {
		if n.Kind() != "Switch" {
			return
		}
		c := comp(n)
		if c == nil {
			return
		}
		target := 0.0
		if c.Checked.Get() {
			target = 1
		}
		if !c.TogglePosed {
			// The first frame after a reload starts where the design says, so
			// loading a page does not slide every switch on.
			c.TogglePosed = true
			c.TogglePos = target
			return
		}
		if c.TogglePos == target {
			return
		}
		c.TogglePos = easeToward(c.TogglePos, target, dt)
		if math.Abs(c.TogglePos-target) < switchSettled {
			c.TogglePos = target
		}
		c.Base().MarkDirty(tree.DirtyPaint)
		moving = true
	})
	return moving
}

// easeToward moves cur toward target by one frame's worth of exponential
// easing. The step depends on the remaining distance, so it decelerates as it
// arrives and reverses without a jump.
func easeToward(cur, target, dt float64) float64 {
	return cur + (target-cur)*(1-math.Exp(-dt/switchTau))
}
