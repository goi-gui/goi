package ui

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

// livePlayer starts a player on the fixture clip and puts it in live mode, which
// is the state a probed stream with no length produces.
func livePlayer(t *testing.T, design string) (*App, *tree.Media, *mediaPlayer) {
	t.Helper()
	a, _, m := mediaApp(t, mediaClip(t), design)
	p, err := a.startPlayer(m.Base().Node())
	if err != nil {
		t.Fatal(err)
	}
	// The probe of the fixture sees a file; a live feed is the same player with
	// no length.
	p.live = true
	m.Live, m.Duration, m.Playing = true, 0, true
	t.Cleanup(a.closeMedia)
	return a, m, p
}

// TestLiveStreamNeverEnds is the core of live playback: the clip ends after a
// second and a half, and the player reopens it instead of stopping or reporting
// an end.
func TestLiveStreamNeverEnds(t *testing.T) {
	a, m, p := livePlayer(t, `Window { Video { id: v width: 320 height: 180 } }`)
	deadline := time.Now().Add(3 * time.Second)
	frames := 0
	for time.Now().Before(deadline) {
		if changed, _ := a.tickMedia(0.05); changed {
			frames++
		}
		time.Sleep(20 * time.Millisecond)
	}
	if m.Ended || p.reachedEnd() {
		t.Fatal("a live feed reported the end of the stream")
	}
	if err := p.err(); err != nil {
		t.Fatalf("the retry after a dropped feed failed: %v", err)
	}
	if frames == 0 {
		t.Fatal("a live player painted no frames across a reconnect")
	}
}

// TestLiveSeekIsIgnored checks that a design cannot scrub a feed with no
// timeline, and that the position it asked for is not applied.
func TestLiveSeekIsIgnored(t *testing.T) {
	a, m, p := livePlayer(t, `Window { Video { id: v width: 320 height: 180 } }`)
	m.Position = 2 * time.Second
	a.seekMedia(m.Base().Node(), 5*time.Second)
	if m.Position != 2*time.Second {
		t.Fatalf("a live feed seeked to %v", m.Position)
	}
	if a.playerFor(m.Base().Node()) != p {
		t.Fatal("a live seek restarted the decoder")
	}
}

// TestLiveBarHasNoTrack checks the control bar: a live feed shows a badge
// instead of a scrubber, so paint and hit-testing agree there is nothing to
// drag.
func TestLiveBarHasNoTrack(t *testing.T) {
	_, m, _ := livePlayer(t, `Window { Video { id: v width: 640 height: 360 } }`)
	r := tree.Rect{W: 640, H: 360}
	lay := paint.MediaLayoutOf(m, r)
	if lay.Bar.H == 0 {
		t.Fatal("a live bar was not laid out")
	}
	if lay.Track.W != 0 || lay.Thumb.W != 0 {
		t.Fatalf("a live feed kept a scrubber: %+v", lay)
	}
	if lay.Button.W == 0 {
		t.Fatal("a live feed lost its play button")
	}
	// The same layout is a file's, for contrast: a scrubber is back.
	m.Live = false
	m.Duration = 4 * time.Second
	if lay := paint.MediaLayoutOf(m, r); lay.Track.W == 0 {
		t.Fatal("a recorded file lost its scrubber")
	}
}
