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

// tweenStep is one leg of a Tween: a duration, a curve and the function that
// writes one frame of progress.
type tweenStep struct {
	dur  time.Duration
	ease Easing
	fn   func(float64)
}

// Tween is a running animation. Steps run one after another, so a move can
// arrive, settle and fade out without a callback between them; Stop ends the
// whole chain where it is.
//
//	app.Tween(180*time.Millisecond, ui.Standard, func(p float64) {
//		sheet.Height.Set(16 + 184*p)
//	}).Then(90*time.Millisecond, ui.Emphasized, func(p float64) {
//		sheet.Opacity.Set(1 - p)
//	})
//
// Create a tween on the UI goroutine (inside Bind or via Post), and stop it
// there too. Animating a numeric property costs one Set per frame; the value is
// written through Prop, so an expression that reads it follows as well.
type Tween struct {
	app   *App
	steps []tweenStep
	// i is the step now running, start its beginning, done the end.
	i     int
	start time.Time
	done  bool
}

// Tween starts an animation: fn runs with eased progress from 0 to 1 over d.
// Then chains another step that starts where this one ends.
func (a *App) Tween(d time.Duration, ease Easing, fn func(float64)) *Tween {
	t := &Tween{app: a, start: time.Now()}
	t.Then(d, ease, fn)
	if a != nil {
		a.markDirty(0)
	}
	return t
}

// Then queues one more step, run once the steps before it finish. It returns
// the tween, so calls chain:
//
//	app.Tween(d1, ui.Standard, first).Then(d2, ui.Emphasized, second)
//
// A step of zero or negative length runs immediately, and a nil fn is skipped.
func (t *Tween) Then(d time.Duration, ease Easing, fn func(float64)) *Tween {
	if t == nil || t.done {
		return t
	}
	if ease == nil {
		ease = Standard
	}
	if d < 0 {
		d = 0
	}
	step := tweenStep{dur: d, ease: ease, fn: fn}
	if d == 0 {
		// No time passes, so the step is already at its end.
		if fn != nil {
			fn(1)
		}
		return t
	}
	t.steps = append(t.steps, step)
	if t.app != nil && !t.app.running(t) {
		t.app.tweens = append(t.app.tweens, t)
	}
	if t.app != nil {
		t.app.markDirty(0)
	}
	return t
}

// Stop ends the tween without calling fn again, the whole chain with it.
func (t *Tween) Stop() {
	if t == nil || t.done {
		return
	}
	t.done = true
	if t.app != nil {
		t.app.markDirty(0)
	}
}

// Animate moves a numeric property to `to` over d, returning the tween. It reads
// the current value at call time, so two overlapping animations blend from
// wherever the property is now instead of jumping.
func (a *App) Animate(p *Prop[float64], to float64, d time.Duration, ease Easing) *Tween {
	if p == nil {
		return a.Tween(0, ease, nil)
	}
	from := p.Get()
	return a.Tween(d, ease, func(t float64) { p.Set(from + (to-from)*t) })
}

// running reports whether t is already in the app's list.
func (a *App) running(t *Tween) bool {
	for _, x := range a.tweens {
		if x == t {
			return true
		}
	}
	return false
}

// advance moves t one frame forward, starting its next step when this one ends.
func (t *Tween) advance(now time.Time) {
	for !t.done && t.i < len(t.steps) {
		s := t.steps[t.i]
		p := float64(now.Sub(t.start)) / float64(s.dur)
		if p < 1 {
			if s.fn != nil {
				s.fn(s.ease(p))
			}
			return
		}
		if s.fn != nil {
			s.fn(s.ease(1))
		}
		// The next step starts where this one was due to end, so a chain keeps
		// the frame clock's timing instead of drifting by a frame each leg.
		t.start = t.start.Add(s.dur)
		t.i++
		if t.i >= len(t.steps) {
			t.done = true
		}
	}
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
	snapshot := append([]*Tween(nil), a.tweens...)
	for _, t := range snapshot {
		if t.done {
			continue
		}
		t.advance(now)
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
