package ui

import (
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/tree"
)

// TestEaseTowardSettlesAndReverses pins the switch motion: it closes the gap
// every step without overshooting, and a target that changes mid-travel turns
// the handle around instead of jumping.
func TestEaseTowardSettlesAndReverses(t *testing.T) {
	pos := 0.0
	prev := -1.0
	for i := 0; i < 40; i++ {
		pos = easeToward(pos, 1, 1.0/60)
		if pos <= prev {
			t.Fatalf("step %d did not advance: %v then %v", i, prev, pos)
		}
		if pos > 1 {
			t.Fatalf("step %d overshot: %v", i, pos)
		}
		prev = pos
	}
	if math.Abs(pos-1) > 0.002 {
		t.Fatalf("after 40 frames pos = %v, want it settled", pos)
	}

	// Half way, flip back: the next step must move down, not restart from 0.
	pos = easeToward(0, 1, 0.15)
	if pos < 0.5 {
		t.Fatalf("one long frame only reached %v", pos)
	}
	back := easeToward(pos, 0, 1.0/60)
	if back >= pos || back < 0 {
		t.Fatalf("reversal went %v -> %v", pos, back)
	}
}

// TestSwitchAnimatesOnToggle covers the wiring: the first tick adopts the
// design's state, a later change of `checked` starts a travel that takes more
// than one frame and ends exactly on the target.
func TestSwitchAnimatesOnToggle(t *testing.T) {
	const src = `Window { Column { Switch { id: sw label: "Wi-Fi" } } }`
	_, win, in := inputFixture(t, src, 480, 320)
	sw := MustByID[*Switch](win, "sw")

	// Idle: the handle sits on the off end and asks for no frames.
	if in.tickSwitches(1.0 / 60) {
		t.Fatal("an untouched switch is already animating")
	}
	if sw.TogglePos != 0 {
		t.Fatalf("initial pos = %v, want 0", sw.TogglePos)
	}

	sw.Checked.Set(true)
	if !in.tickSwitches(1.0 / 60) {
		t.Fatal("checked did not start a travel")
	}
	if sw.TogglePos <= 0 || sw.TogglePos >= 1 {
		t.Fatalf("pos = %v after one frame, want it in flight", sw.TogglePos)
	}

	frames := 1
	for in.tickSwitches(1.0 / 60) {
		frames++
		if frames > 200 {
			t.Fatal("the handle never landed")
		}
	}
	if frames < 2 {
		t.Fatalf("landed in %d frames, that is not an animation", frames)
	}
	if sw.TogglePos != 1 {
		t.Fatalf("final pos = %v, want exactly 1", sw.TogglePos)
	}

	// And back off again.
	sw.Checked.Set(false)
	sw.Base().MarkDirty(tree.DirtyPaint)
	in.tickSwitches(1.0 / 60)
	if sw.TogglePos >= 1 {
		t.Fatal("turning off did not move the handle")
	}
}

// TestSwitchStartsAtTheDesignState guards against a startup animation: a switch
// that begins checked adopts 1 on the first tick instead of sliding in.
func TestSwitchStartsAtTheDesignState(t *testing.T) {
	const src = `Window { Column { Switch { id: sw checked: true } } }`
	_, win, in := inputFixture(t, src, 480, 320)
	sw := MustByID[*Switch](win, "sw")
	if in.tickSwitches(1.0 / 60) {
		t.Fatal("a checked switch animated on the first frame")
	}
	if sw.TogglePos != 1 || !sw.TogglePosed {
		t.Fatalf("pos = %v posed = %v, want 1 and true", sw.TogglePos, sw.TogglePosed)
	}
}
