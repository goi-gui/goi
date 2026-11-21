package paint

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/tree"
)

// TestHeroFliesTheDestinationContent: switching which Hero is visible starts a
// flight at the old Hero's box and paints the destination's own content, so the
// last frame is already the real Hero and nothing pops when it lands.
func TestHeroFliesTheDestinationContent(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Stack {
			Hero {
				id:       small
				tag:      "pic"
				duration: 100ms
				Rectangle { width: 50 height: 50 background: #00ff00 }
			}
			Hero {
				id:       big
				tag:      "pic"
				duration: 100ms
				visible:  false
				Rectangle { width: 200 height: 100 background: #ff0000 }
			}
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	layout.New(nil).Layout(tr.Root, 400, 300)
	l := &List{}
	l.Build(tr, 400, 300)

	sm, _ := tr.ByID("small")
	bg, _ := tr.ByID("big")
	sm.Base().SetVisible(false)
	bg.Base().SetVisible(true)
	layout.New(nil).Layout(tr.Root, 400, 300)
	l.Clock = 0
	l.Build(tr, 400, 300) // starts the flight at the old box
	l.Clock = 50 * time.Millisecond
	l.Build(tr, 400, 300) // halfway through the 100ms flight

	var old, destination bool
	for _, op := range l.Ops {
		if op.Kind != Fill {
			continue
		}
		switch op.Color {
		case 0x00FF00FF:
			old = true
		case 0xFF0000FF:
			destination = true
		}
	}
	if old {
		t.Fatal("the old Hero content should be hidden during the flight")
	}
	if !destination {
		t.Fatal("the flight does not paint the destination content")
	}
	if !l.Animating {
		t.Fatal("the flight did not keep the frame loop awake")
	}
}
