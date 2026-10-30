package ui

import (
	"testing"
	"time"

	"github.com/ksckaan1/goi/internal/tree"
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
	stop()
}

// TestTweenStop ends an animation without running it to the end.
func TestTweenStop(t *testing.T) {
	a := newHeadlessApp()
	comp := &tree.Component{}
	p := &comp.Spacing
	stop := a.Animate(p, 100, time.Second, Linear)
	a.tickTweens()
	stop()
	if a.tickTweens() {
		t.Fatal("a stopped tween still runs")
	}
	if p.Get() >= 100 {
		t.Fatalf("value jumped to the target: %v", p.Get())
	}
}
