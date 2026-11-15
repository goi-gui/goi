package ui

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/tree"
)

// TestTweenRunsToCompletion checks the contract: 0 at the start, 1 at the end,
// monotonic in between, and the app stays awake only while it runs.
func TestTweenRunsToCompletion(t *testing.T) {
	a := newHeadlessApp()
	a.win = &Window{}
	comp := &tree.Component{}
	p := &comp.Spacing

	var seen []float64
	stop := a.Animate(p, 100, 30*time.Millisecond, Linear)
	if !a.tickTweens() {
		t.Fatal("a fresh tween did not keep the app awake")
	}
	seen = append(seen, p.Get())
	time.Sleep(15 * time.Millisecond)
	a.tickTweens()
	seen = append(seen, p.Get())
	time.Sleep(25 * time.Millisecond)
	if a.tickTweens() {
		t.Fatal("a finished tween still asked for frames")
	}
	seen = append(seen, p.Get())
	if seen[0] > seen[1] || seen[1] > seen[2] {
		t.Fatalf("progress is not monotonic: %v", seen)
	}
	if got := p.Get(); got != 100 {
		t.Fatalf("final value = %v, want 100", got)
	}
	stop.Stop()
}

// TestTweenStop ends an animation without running it to the end.
func TestTweenStop(t *testing.T) {
	a := newHeadlessApp()
	comp := &tree.Component{}
	p := &comp.Spacing
	stop := a.Animate(p, 100, time.Second, Linear)
	a.tickTweens()
	stop.Stop()
	if a.tickTweens() {
		t.Fatal("a stopped tween still runs")
	}
	if p.Get() >= 100 {
		t.Fatalf("value jumped to the target: %v", p.Get())
	}
}

// TestTweenChainsSteps: the second step starts where the first ended, runs
// once, and the chain reports itself done at the end.
func TestTweenChainsSteps(t *testing.T) {
	a := newHeadlessApp()
	comp := &tree.Component{}
	p := &comp.Spacing
	var calls []float64
	tw := a.Tween(10*time.Millisecond, Linear, func(f float64) {
		p.Set(10 * f)
	}).Then(10*time.Millisecond, Linear, func(f float64) {
		calls = append(calls, 10+10*f)
		p.Set(10 + 10*f)
	})
	if !a.tickTweens() {
		t.Fatal("a chained tween did not keep the app awake")
	}
	time.Sleep(40 * time.Millisecond)
	if a.tickTweens() {
		t.Fatal("a finished chain still asked for frames")
	}
	if p.Get() != 20 {
		t.Fatalf("final value = %v, want the end of the second step", p.Get())
	}
	if len(calls) == 0 || calls[len(calls)-1] != 20 {
		t.Fatalf("second step ran with %v, want it to finish at 20", calls)
	}
	for i := 1; i < len(calls); i++ {
		if calls[i] < calls[i-1] {
			t.Fatalf("the second step went backwards: %v", calls)
		}
	}
	if !tw.done {
		t.Fatal("the chain never reported itself done")
	}
}

// TestTweenStopEndsTheChain: stopping during the first step leaves the rest of
// the chain unscheduled.
func TestTweenStopEndsTheChain(t *testing.T) {
	a := newHeadlessApp()
	comp := &tree.Component{}
	p := &comp.Spacing
	ran := false
	tw := a.Tween(10*time.Millisecond, Linear, func(f float64) { p.Set(f) }).
		Then(10*time.Millisecond, Linear, func(float64) { ran = true })
	tw.Stop()
	if a.tickTweens() {
		t.Fatal("a stopped chain still runs")
	}
	time.Sleep(15 * time.Millisecond)
	a.tickTweens()
	if ran {
		t.Fatal("a stopped chain ran its next step")
	}
}
