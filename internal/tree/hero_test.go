package tree

import (
	"testing"
	"time"
)

// TestHeroFlightProgress: the tag's old and new nodes start a flight, and its
// box interpolates between the two over the duration. The old content is what
// paint draws during the flight.
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
