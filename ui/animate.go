package ui

import "time"

// Easing maps linear progress in [0,1] to a shaped one.
type Easing func(float64) float64

// Easing functions, named after the M3 motion set they approximate.
func Linear(t float64) float64 { return t }

// Standard is the M3 "standard" curve: a fast start with a soft landing.
func Standard(t float64) float64 { return 1 - (1-t)*(1-t)*(1-t) }

// Emphasized is slower at both ends, for a large change such as a page.
func Emphasized(t float64) float64 {
	if t < 0.5 {
		return 2 * t * t
	}
	u := -2*t + 2
	return 1 - u*u/2
}

// tween is one running animation. The app ticks the list before the frame
// check; a tween that writes a property marks the tree dirty, which is what
// asks for the next frame, and a running tween keeps the loop awake by itself.
type tween struct {
	start time.Time
	dur   time.Duration
	ease  Easing
	fn    func(float64)
	done  bool
}

// Tween runs fn with eased progress from 0 to 1 over d, then stops. The
// returned stop function ends it early without calling fn again.
//
//	app.Tween(180*time.Millisecond, ui.Standard, func(p float64) {
//		sheet.Height.Set(16 + 184*p)
//	})
//
// Create a tween on the UI goroutine (inside Bind or via Post), and call stop
// there too. Animating a numeric property costs one Set per frame; the value is
// written through Prop, so an expression that reads it follows as well.
func (a *App) Tween(d time.Duration, ease Easing, fn func(float64)) (stop func()) {
	if fn == nil {
		return func() {}
	}
	if ease == nil {
		ease = Standard
	}
	if d <= 0 {
		fn(1)
		return func() {}
	}
	t := &tween{start: time.Now(), dur: d, ease: ease, fn: fn}
	a.tweens = append(a.tweens, t)
	a.markDirty(0)
	return func() {
		t.done = true
		a.markDirty(0)
	}
}

// Animate moves a numeric property to `to` over d, returning stop. It reads the
// current value at call time, so two overlapping animations blend from wherever
// the property is now instead of jumping.
func (a *App) Animate(p *Prop[float64], to float64, d time.Duration, ease Easing) (stop func()) {
	if p == nil {
		return func() {}
	}
	from := p.Get()
	return a.Tween(d, ease, func(t float64) { p.Set(from + (to-from)*t) })
}

// tickTweens advances every live tween and reports whether any remain. It runs
// before the frame check, so a tween keeps the loop from sleeping.
func (a *App) tickTweens() bool {
	if len(a.tweens) == 0 {
		return false
	}
	now := time.Now()
	// Calls below may stop (or start) tweens, so iterate over the snapshot and
	// prune afterwards rather than mutating the slice in place.
	live := a.tweens[:0]
	for _, t := range a.tweens {
		if !t.done {
			live = append(live, t)
		}
	}
	a.tweens = live
	snapshot := append([]*tween(nil), a.tweens...)
	for _, t := range snapshot {
		if t.done {
			continue
		}
		p := float64(now.Sub(t.start)) / float64(t.dur)
		if p >= 1 {
			p = 1
			t.done = true
		}
		t.fn(t.ease(p))
	}
	live = a.tweens[:0]
	for _, t := range a.tweens {
		if !t.done {
			live = append(live, t)
		}
	}
	a.tweens = live
	return len(a.tweens) > 0
}
