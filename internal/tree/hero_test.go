package tree

import (
	"testing"
	"time"
)

// TestHeroFlightProgress: the tag's old and new nodes start a flight, and its
// box interpolates between the two over the duration.
func TestHeroFlightProgress(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Stack {
			Hero {
				id:  small
				tag: "pic"
				Rectangle { width: 50 height: 50 }
			}
			Hero {
				id:  big
				tag: "pic"
				Rectangle { width: 200 height: 100 }
			}
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	small, _ := tr.ByID("small")
	big, _ := tr.ByID("big")
	smallHero := small.Base().Node().(*Hero)
	bigHero := big.Base().Node().(*Hero)
	old := Rect{X: 0, Y: 0, W: 50, H: 50}
	next := Rect{X: 0, Y: 0, W: 200, H: 100}

	tr.BeginHeroFrame()
	if _, mode, _ := tr.HeroAt(smallHero, small.Base(), old, 0); mode != HeroNormal {
		t.Fatalf("first page mode = %v", mode)
	}
	tr.EndHeroFrame()

	tr.BeginHeroFrame()
	_, mode, cur := tr.HeroAt(bigHero, big.Base(), next, 0)
	if mode != HeroFly || cur != old {
		t.Fatalf("flight start = %v at %v, want fly from %v", mode, cur, old)
	}
	tr.EndHeroFrame()

	tr.BeginHeroFrame()
	_, mode, cur = tr.HeroAt(bigHero, big.Base(), next, 50*time.Millisecond)
	if mode != HeroFly || cur.W <= 50 || cur.W >= 200 {
		t.Fatalf("flight middle = %v at %v", mode, cur)
	}
	tr.EndHeroFrame()
}

func TestHeroCurveAndArc(t *testing.T) {
	// fastOutSlowIn covers most of the distance by the middle, then settles.
	if p := heroProgress(&HeroFlight{Duration: time.Second}, 500*time.Millisecond); p < 0.7 || p > 0.85 {
		t.Fatalf("mid progress = %v, want a fast-out-slow-in curve", p)
	}
	// A diagonal flight bends away from the straight midpoint.
	f := &HeroFlight{
		FromRect: Rect{X: 0, Y: 0, W: 100, H: 100},
		ToRect:   Rect{X: 200, Y: 200, W: 100, H: 100},
	}
	r := heroRect(f, 0.5)
	center := r.X + r.W/2
	if center == 150 {
		t.Fatalf("diagonal flight went straight: %v", r)
	}
	// Aligned flights stay straight, so a tile that only changes width or
	// position on one axis does not wobble.
	f = &HeroFlight{
		FromRect: Rect{X: 0, Y: 0, W: 100, H: 100},
		ToRect:   Rect{X: 200, Y: 0, W: 100, H: 100},
	}
	if r := heroRect(f, 0.5); r.Y+r.H/2 != 50 {
		t.Fatalf("aligned flight bent: %v", r)
	}
}
