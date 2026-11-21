package tree

import (
	"math"
	"time"
)

// defaultHeroDuration is how long a shared element flies when the design does
// not name a duration.
const defaultHeroDuration = 300 * time.Millisecond

// Hero is a shared-element wrapper. Two Heroes with the same tag on the two
// sides of a page switch share one flight: the destination content starts at the
// old box and eases into its own. The wrapper hugs its children; it draws
// nothing of its own.
type Hero struct {
	Container
	Tag      Prop[string]
	Duration Prop[time.Duration]
}

// Kind implements Node.
func (*Hero) Kind() string { return "Hero" }

func (h *Hero) namedProps() []NamedProp {
	return append(h.Container.namedProps(),
		StringProp(&h.NodeBase, "tag", &h.Tag),
		DurationProp(&h.NodeBase, "duration", DirtyPaint, &h.Duration),
	)
}

// heroSnapshot is one visible Hero in a painted frame. The next frame compares
// the tag's node to know that a page switched under it.
type heroSnapshot struct {
	node *NodeBase
	rect Rect
}

// HeroFlight is one shared element in flight. From and To are the two Hero
// nodes; FromRect is where the old one was drawn, ToRect where the new one is
// laid out.
type HeroFlight struct {
	Tag      string
	From     *NodeBase
	To       *NodeBase
	FromRect Rect
	ToRect   Rect
	Start    time.Duration
	Duration time.Duration
}

// HeroMode tells paint what to do with a Hero this frame.
type HeroMode uint8

// Hero modes.
const (
	// HeroNormal paints the Hero in place.
	HeroNormal HeroMode = iota
	// HeroFly removes the Hero from its page and adds the flight copy.
	HeroFly
	// HeroHide removes the Hero because it is the flight's source.
	HeroHide
)

// BeginHeroFrame starts collecting the visible Heroes for one painted frame.
func (t *Tree) BeginHeroFrame() {
	if t == nil {
		return
	}
	t.heroNow = map[string]heroSnapshot{}
}

// EndHeroFrame remembers this frame's Heroes and drops flights whose
// destination did not paint (for example, the page was switched back).
func (t *Tree) EndHeroFrame() {
	if t == nil {
		return
	}
	for tag, f := range t.heroFlights {
		if snap, ok := t.heroNow[tag]; !ok || snap.node != f.To {
			delete(t.heroFlights, tag)
		}
	}
	t.heroPrev = t.heroNow
	t.heroNow = nil
}

// HeroAt places one visible Hero in the frame. It starts a flight when the tag
// moved to a different node since the last frame, and returns the mode paint
// should use and the flight's current box.
func (t *Tree) HeroAt(h *Hero, n *NodeBase, r Rect, clock time.Duration) (*HeroFlight, HeroMode, Rect) {
	if t == nil || h == nil || n == nil {
		return nil, HeroNormal, Rect{}
	}
	tag := h.Tag.Get()
	if tag == "" || t.heroNow == nil {
		return nil, HeroNormal, Rect{}
	}
	if t.heroFlights == nil {
		t.heroFlights = map[string]*HeroFlight{}
	}
	if f := t.heroFlights[tag]; f != nil {
		switch {
		case f.To == n:
			if p := heroProgress(f, clock); p >= 1 {
				delete(t.heroFlights, tag)
				break
			} else {
				t.heroNow[tag] = heroSnapshot{n, r}
				return f, HeroFly, heroRect(f, p)
			}
		case f.From == n:
			return f, HeroHide, Rect{}
		default:
			// A third Hero took the tag; the old flight cannot land.
			delete(t.heroFlights, tag)
		}
	}
	if prev, ok := t.heroPrev[tag]; ok && prev.node != n {
		d := h.Duration.Get()
		if d <= 0 {
			d = defaultHeroDuration
		}
		f := &HeroFlight{Tag: tag, From: prev.node, To: n, FromRect: prev.rect, ToRect: r, Start: clock, Duration: d}
		t.heroFlights[tag] = f
		t.heroNow[tag] = heroSnapshot{n, r}
		return f, HeroFly, prev.rect
	}
	t.heroNow[tag] = heroSnapshot{n, r}
	return nil, HeroNormal, Rect{}
}

// heroProgress is the eased fraction of a flight at clock.
func heroProgress(f *HeroFlight, clock time.Duration) float64 {
	if f.Duration <= 0 {
		return 1
	}
	p := float64(clock-f.Start) / float64(f.Duration)
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	// Material's fast-out-slow-in curve: the flight leaves gently, covers most
	// of the distance in the middle, and settles instead of stopping abruptly.
	return cubicBezier(0.4, 0, 0.2, 1, p)
}

// heroRect moves the centre along a slight arc and the size linearly. Flutter's
// Material rect tween does the same: an element that changes both axes sweeps
// around the page instead of cutting a straight diagonal. Aligned flights stay
// straight, where an arc would only look like a wobble.
func heroRect(f *HeroFlight, p float64) Rect {
	w := f.FromRect.W + (f.ToRect.W-f.FromRect.W)*p
	h := f.FromRect.H + (f.ToRect.H-f.FromRect.H)*p
	fx, fy := f.FromRect.X+f.FromRect.W/2, f.FromRect.Y+f.FromRect.H/2
	tx, ty := f.ToRect.X+f.ToRect.W/2, f.ToRect.Y+f.ToRect.H/2
	cx, cy := (fx+tx)/2, (fy+ty)/2
	if dx, dy := tx-fx, ty-fy; math.Abs(dx) > 1 && math.Abs(dy) > 1 {
		d := math.Hypot(dx, dy)
		// A shallow bend, so the path reads as an arc and not a loop.
		bend := d * 0.12
		cx -= dy / d * bend
		cy += dx / d * bend
	}
	x := (1-p)*(1-p)*fx + 2*(1-p)*p*cx + p*p*tx
	y := (1-p)*(1-p)*fy + 2*(1-p)*p*cy + p*p*ty
	return Rect{X: x - w/2, Y: y - h/2, W: w, H: h}
}
