package paint

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/subtitle"
	"github.com/goi-gui/goi/internal/tree"
)

// captionPlayer builds a video from a design, gives it one caption, lays it out
// and paints it. The cues come from the ui layer in a real app, so the design
// cannot express them and the test sets them on the node.
func captionPlayer(t *testing.T, props string) (*tree.Media, *List) {
	t.Helper()
	src := `Window { Video { id: v width: 640 height: 360 ` + props + ` } }`
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	var m *tree.Media
	for _, n := range tr.All() {
		if mm, ok := tree.MediaNode(n); ok {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("no media node")
	}
	m.Cues = []subtitle.Cue{{Start: 0, End: 5 * time.Second, Text: "hello"}}
	m.Position = 0
	m.ControlsShown = true
	layout.New(nil).Layout(tr.Root, 700, 400)
	l := &List{}
	l.Build(tr, 700, 400)
	return m, l
}

// TestSubtitleStylingIsApplied is the look: size, colour, weight, plate and
// shadow all reach the caption op instead of the hardcoded white-on-scrim.
func TestSubtitleStylingIsApplied(t *testing.T) {
	_, l := captionPlayer(t, `
		subtitles: "x.srt"
		subtitleFontSize: 30
		subtitleColor: #ff0000ff
		subtitleFontWeight: bold
		subtitleBackground: #00000080
		subtitleShadow: 0 3 6 #00ff00ff
	`)

	var textOp *Op
	plate := false
	for i := range l.Ops {
		op := &l.Ops[i]
		if op.Kind == Text && op.Text == "hello" {
			textOp = op
		}
		if op.Kind == Fill && op.Color == 0x00000080 {
			plate = true
		}
	}
	if textOp == nil {
		t.Fatal("the caption did not draw")
	}
	if textOp.FontSize != 30 {
		t.Fatalf("caption size = %v, want 30", textOp.FontSize)
	}
	if textOp.Color != 0xff0000ff {
		t.Fatalf("caption colour = %#x", textOp.Color)
	}
	if textOp.Weight != 700 {
		t.Fatalf("caption weight = %v, want 700 for bold", textOp.Weight)
	}
	if len(textOp.TextShadows) != 1 {
		t.Fatalf("caption shadows = %d, want 1", len(textOp.TextShadows))
	}
	if got := textOp.TextShadows[0].Color; got != 0x00ff00ff {
		t.Fatalf("shadow colour = %#x", got)
	}
	if !plate {
		t.Fatal("subtitleBackground drew no plate")
	}
}

// TestSubtitleDefaultsStayReadable keeps a design that names no styling on the
// white default with no plate, which is what makes captions readable over any
// frame.
func TestSubtitleDefaultsStayReadable(t *testing.T) {
	_, l := captionPlayer(t, `subtitles: "x.srt"`)
	var found bool
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "hello" {
			found = true
			if op.Color != 0xFFFFFFFF {
				t.Fatalf("default caption colour = %#x, want white", op.Color)
			}
			if len(op.TextShadows) != 0 {
				t.Fatal("a default caption drew a shadow")
			}
		}
		if op.Kind == Fill && op.Color == 0x00000080 {
			t.Fatal("a default caption drew a plate")
		}
	}
	if !found {
		t.Fatal("the caption did not draw")
	}
}

// TestControlColoursRecolourTheBar checks the three knobs: icons, text and the
// scrubber each take their own colour.
func TestControlColoursRecolourTheBar(t *testing.T) {
	_, l := captionPlayer(t, `
		controlsIconColor: #ff0000ff
		controlsTextColor: #00ff00ff
		controlsTrackColor: #0000ffff
	`)

	icon, text, track, thumb := false, false, false, false
	for _, op := range l.Ops {
		switch op.Kind {
		case Text:
			if op.Text == "play_arrow" && op.Color == 0xff0000ff {
				icon = true
			}
			if op.Text == "0:00 / 0:00" && op.Color == 0x00ff00ff {
				text = true
			}
		case Fill:
			if op.Color == 0x0000ffff {
				track = true
				if op.Rect[2] == 12 {
					thumb = true
				}
			}
		}
	}
	if !icon {
		t.Fatal("the play icon did not take controlsIconColor")
	}
	if !text {
		t.Fatal("the clock did not take controlsTextColor")
	}
	if !track || !thumb {
		t.Fatalf("the scrubber did not take controlsTrackColor (track %v thumb %v)", track, thumb)
	}
}

// TestUnstyledBarKeepsTheTheme guards the zero values: a design that recolours
// nothing must not pick up an accidental colour.
func TestUnstyledBarKeepsTheTheme(t *testing.T) {
	_, l := captionPlayer(t, "")
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == "play_arrow" &&
			(op.Color == 0xff0000ff || op.Color == 0x00ff00ff || op.Color == 0x0000ffff) {
			t.Fatalf("an unstyled bar picked up a design colour %#x", op.Color)
		}
	}
}
