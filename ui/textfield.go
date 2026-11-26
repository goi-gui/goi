package ui

import (
	"math"
	"time"

	"github.com/goi-gui/goi/internal/tree"
)

// fieldLabelTau shapes a text field label's travel: the label eases toward the
// target every frame rather than running from a start time, so a focus that
// comes and goes mid-way turns around instead of restarting. 45 ms settles in
// about 150 ms at 60 Hz.
const fieldLabelTau = 0.045 // seconds

// fieldLabelSettled is how close the label must be to snap onto its target, so
// an animation always finishes and the loop can sleep.
const fieldLabelSettled = 0.002

// tickFieldLabels advances every floating label toward where its field puts it
// and reports whether one is still moving. It runs before the frame check, so
// the travel keeps asking for frames until it lands.
func (i *interactor) tickFieldLabels(dt float64) bool {
	if i.tree == nil || dt <= 0 {
		return false
	}
	moving := false
	i.tree.Walk(func(n tree.Node) {
		if n.Kind() != "TextField" && n.Kind() != "TextArea" {
			return
		}
		c := comp(n)
		if c == nil {
			return
		}
		target := 0.0
		if c.LabelFloats() {
			target = 1
		}
		if !c.LabelPosed {
			// The first frame after a reload starts where the design says, so
			// loading a page does not animate every label up.
			c.LabelPosed = true
			c.LabelFloat = target
			return
		}
		pos := c.LabelFloat
		if math.Abs(target-pos) <= fieldLabelSettled {
			if pos != target {
				c.LabelFloat = target
				c.Base().MarkDirty(tree.DirtyPaint)
			}
			return
		}
		c.LabelFloat = pos + (target-pos)*(1-math.Exp(-dt/fieldLabelTau))
		c.Base().MarkDirty(tree.DirtyPaint)
		moving = true
	})
	return moving
}

// caretBlinkHalf is how long the caret stays visible or hidden while a field
// has focus.
const caretBlinkHalf = 0.25 // seconds

// tickCaretBlink advances the focused text field's caret blink and reports
// whether it toggled, plus how long until the next toggle is due. The wait lets
// the loop sleep between blinks instead of redrawing at the display rate.
func (i *interactor) tickCaretBlink(dt float64) (bool, time.Duration) {
	c := i.focus
	if c == nil || !isTextField(c) || !visibleInTree(c.Base().Node()) {
		return false, 0
	}
	if dt > 0 {
		c.CaretBlinkPhase += dt
	}
	changed := false
	for c.CaretBlinkPhase >= caretBlinkHalf {
		c.CaretBlinkPhase -= caretBlinkHalf
		c.CaretBlink = !c.CaretBlink
		changed = true
	}
	if changed {
		c.Base().MarkDirty(tree.DirtyPaint)
	}
	wait := time.Duration((caretBlinkHalf - c.CaretBlinkPhase) * float64(time.Second))
	if wait <= 0 {
		wait = time.Millisecond
	}
	return changed, wait
}
