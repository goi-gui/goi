package ui

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/img"
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

// TestLivePaceFollowsTheQueue guards the live clock: a deep queue plays faster
// than a shallow one, which is what settles the picture at the source's real
// rate. Pacing at the container's rate alone drained the buffer one frame at a
// time (a 24 fps feed reported as 25) and every drain froze the picture.
func TestLivePaceFollowsTheQueue(t *testing.T) {
	p := &mediaPlayer{info: mediaInfoFor(25)}
	p.frames = make(chan []byte, 100)
	base := p.framePeriod()

	p.frames <- make([]byte, 1) // nearly empty: play slower
	shallow := p.livePeriod()
	for len(p.frames) < cap(p.frames) {
		p.frames <- make([]byte, 1)
	}
	deep := p.livePeriod()

	if shallow <= base {
		t.Fatalf("an empty queue paced at %v, want slower than %v", shallow, base)
	}
	if deep >= base {
		t.Fatalf("a full queue paced at %v, want faster than %v", deep, base)
	}
	// The correction stays bounded, or the picture would sprint or crawl.
	if deep < base*8/10 || shallow > base*16/10 {
		t.Fatalf("pace out of bounds: deep %v shallow %v base %v", deep, shallow, base)
	}
}

// TestLiveFeedUsesALiveBuffer checks the default depth: a live feed's segments
// arrive with gaps, so the file default of 1.5s is too shallow to play through
// them.
func TestLiveFeedUsesALiveBuffer(t *testing.T) {
	a, _, m := mediaApp(t, mediaClip(t), `Window { Video { id: v width: 320 height: 180 } }`)
	m.Live = true
	p, err := a.startPlayer(m.Base().Node())
	if err != nil {
		t.Fatal(err)
	}
	defer a.closeMedia()
	if p.buffer < liveDefaultBuffer {
		t.Fatalf("live buffer = %.1fs, want at least %.1fs", p.buffer, liveDefaultBuffer)
	}
	// A design that asked for a depth keeps it.
	_, _, m2 := mediaApp(t, mediaClip(t), `Window { Video { id: v2 width: 320 height: 180 buffer: 1 } }`)
	m2.Live = true
	p2, err := a.startPlayer(m2.Base().Node())
	if err != nil {
		t.Fatal(err)
	}
	if p2.buffer != 1 {
		t.Fatalf("the design's 1s buffer became %.1fs", p2.buffer)
	}
}

// TestFileFramesKeepTheirCadence is the stutter guard: a 25 fps file must show
// 25 frames a second even when the loop ticks 21 times a second. The deadline
// moves one period at a time, so the display rate cannot quantize the stream.
func TestFileFramesKeepTheirCadence(t *testing.T) {
	a, win := reloadApp(t, `Window { Video { id: v width: 320 height: 180 } }`)
	v := MustByID[*Video](win, "v")
	m := &v.Media
	m.Playing = true
	p := &mediaPlayer{
		node:   v,
		m:      m,
		info:   mediaInfoFor(25),
		frame:  &img.Data{W: 2, H: 2, Pix: make([]byte, 16)},
		frames: make(chan []byte, 64),
		free:   make(chan []byte, 64),
	}
	for i := 0; i < 64; i++ {
		p.frames <- make([]byte, 16)
	}
	v.Data = p.frame
	a.media = []*mediaPlayer{p}
	// One period late, so the first tick shows a frame.
	p.nextFrame = time.Now().Add(-p.framePeriod())

	shown := 0
	start := time.Now()
	prev := p.nextFrame
	for time.Since(start) < 400*time.Millisecond {
		before := v.Data.Gen
		a.tickMedia(0)
		if v.Data.Gen != before {
			shown++
			// The deadline advanced by exactly one period, measured from the old
			// deadline rather than from the tick.
			// One period, or one period plus the lateness of this tick; never a
			// whole extra frame from "one period from now".
			if d := p.nextFrame.Sub(prev); d > 2*p.framePeriod() {
				t.Fatalf("deadline moved %v, want about one %v period", d, p.framePeriod())
			}
			prev = p.nextFrame
		}
		time.Sleep(8 * time.Millisecond)
	}
	// The loop ticks at 120 fps here, so a quantized player would show a frame
	// on every tick; 400ms of a 25 fps clip is ten frames.
	if shown < 8 || shown > 12 {
		t.Fatalf("showed %d frames in 400ms of a 25 fps clip", shown)
	}
}
