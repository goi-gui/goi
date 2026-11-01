package ui

import (
	"testing"
	"time"
)

// A capped app must not burst frames to catch up: the deadline moves one period
// per frame, whatever the frame actually took.
func TestPaceHoldsToMaxFPS(t *testing.T) {
	d := &gpuDriver{app: &App{vsync: false, maxFPS: 120}}
	period := time.Second / 120
	start := time.Now()

	d.pace(start)
	if got := d.frameAt.Sub(start); got != period {
		t.Fatalf("first deadline %v, want one period %v", got, period)
	}
	d.pace(start)
	if got := d.frameAt.Sub(start); got != 2*period {
		t.Fatalf("second deadline %v, want two periods %v", got, 2*period)
	}
}

// Without a cap the loop is left alone, so vsync stays the pacer.
func TestPaceOffByDefault(t *testing.T) {
	d := &gpuDriver{app: &App{vsync: true}}
	d.pace(time.Now())
	if !d.frameAt.IsZero() {
		t.Fatalf("frameAt = %v, want zero when no cap is set", d.frameAt)
	}
}
