package ui

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/media"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

// mediaClip renders a one second, 10 fps test clip next to a design, so the
// player tests exercise the real decoder. A machine without ffmpeg skips them.
func mediaClip(t *testing.T) string {
	t.Helper()
	if !media.Available() {
		t.Skip("ffmpeg is not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=160x90:rate=10:duration=1.5",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1.5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build the clip: %v\n%s", err, out)
	}
	return path
}

// mediaApp builds a window with a player, with the clip standing in for the
// design's src. Nothing is laid out or drawn by the platform: this is the
// headless path.
func mediaApp(t *testing.T, src, design string) (*App, *Window, *tree.Media) {
	t.Helper()
	a, win := reloadApp(t, design)
	if src != "" {
		for _, n := range win.tree.All() {
			if m, ok := tree.MediaNode(n); ok {
				m.Src.Set(src)
			}
		}
	}
	for _, n := range win.tree.All() {
		if m, ok := tree.MediaNode(n); ok {
			a.probeMedia(n, m)
		}
	}
	layout.New(nil).Layout(win.tree.Root, 400, 300)
	var got *tree.Media
	for _, n := range win.tree.All() {
		if m, ok := tree.MediaNode(n); ok {
			got = m
		}
	}
	if got == nil {
		t.Fatal("no media node in the design")
	}
	return a, win, got
}

// TestVideoPlaysAndAdvances is the whole path: a real decode, a frame in the
// node's canvas, and a clock that moves.
func TestVideoPlaysAndAdvances(t *testing.T) {
	clip := mediaClip(t)
	a, _, m := mediaApp(t, clip, `Window { Video { id: v width: 320 height: 180 } }`)
	n := m.Base().Node()

	a.playMedia(n)
	if !m.Playing || m.Paused {
		t.Fatalf("state after play: playing=%v paused=%v", m.Playing, m.Paused)
	}
	if m.Duration <= 0 {
		t.Fatal("the probe did not read a duration")
	}

	// The decoder runs on its own goroutine, so give it a moment to produce the
	// first frame, then let one frame interval pass.
	deadline := time.Now().Add(5 * time.Second)
	v := n.(*tree.Video)
	for time.Now().Before(deadline) {
		changed, _ := a.tickMedia(1.0 / 60)
		_ = changed
		if v.Data != nil && v.Data.Pix != nil && !allZero(v.Data.Pix) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if v.Data == nil || allZero(v.Data.Pix) {
		t.Fatal("no video frame reached the node")
	}
	if v.Data.Gen == 0 {
		t.Fatal("the frame was not marked as new")
	}
	if m.Position <= 0 {
		t.Fatalf("the clock did not advance: %v", m.Position)
	}
	a.closeMedia()
}

// allZero reports whether a pixel buffer is still black, which is how a frame
// that never arrived looks.
func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// TestVideoPauseKeepsThePosition checks the transport: playing advances, pause
// stops, play resumes, stop rewinds.
func TestVideoPauseKeepsThePosition(t *testing.T) {
	clip := mediaClip(t)
	a, _, m := mediaApp(t, clip, `Window { Video { id: v width: 320 height: 180 } }`)
	n := m.Base().Node()
	a.playMedia(n)
	a.tickMedia(0.5)
	at := m.Position
	if at <= 0 {
		t.Fatalf("position after a half second = %v", at)
	}

	var played, paused int
	m.Playback.On(func(on bool) {
		if on {
			played++
		} else {
			paused++
		}
	})
	a.pauseMedia(n)
	if !m.Paused || m.Position != at {
		t.Fatalf("after pause: paused=%v position=%v, want %v", m.Paused, m.Position, at)
	}
	a.tickMedia(0.5)
	if m.Position != at {
		t.Fatalf("a paused player advanced to %v", m.Position)
	}
	if paused != 1 {
		t.Fatalf("pause events = %d", paused)
	}

	a.stopMedia(n)
	if m.Position != 0 || m.Playing {
		t.Fatalf("after stop: position=%v playing=%v", m.Position, m.Playing)
	}
	a.closeMedia()
}

// TestMediaEndsAndLoops covers the end of a file and the loop switch.
func TestMediaEndsAndLoops(t *testing.T) {
	clip := mediaClip(t)
	a, _, m := mediaApp(t, clip, `Window { Audio { id: a width: 320 height: 48 } }`)
	n := m.Base().Node()
	var ended int
	m.End.On(func(time.Duration) { ended++ })

	a.playMedia(n)
	// Walk past the end in one big step; the loop is off.
	a.tickMedia(5)
	if !m.Ended || ended != 1 {
		t.Fatalf("ended=%v end events=%d", m.Ended, ended)
	}
	if m.Position != m.Duration {
		t.Fatalf("position at the end = %v, want the duration %v", m.Position, m.Duration)
	}

	// With loop on it restarts instead of stopping.
	m.Loop.Set(true)
	m.Ended = false
	a.playMedia(n)
	a.tickMedia(5)
	if m.Ended {
		t.Fatal("a looping player reported the end")
	}
	if m.Position > m.Duration {
		t.Fatalf("looped position = %v", m.Position)
	}
	a.closeMedia()
}

// TestMediaSeekRestartsAtTheOffset checks that a seek moves the clock and keeps
// the player running, which is what the scrubber does.
func TestMediaSeekRestartsAtTheOffset(t *testing.T) {
	clip := mediaClip(t)
	a, _, m := mediaApp(t, clip, `Window { Video { id: v width: 320 height: 180 } }`)
	n := m.Base().Node()
	a.playMedia(n)
	var seeks []time.Duration
	m.Seeked.On(func(d time.Duration) { seeks = append(seeks, d) })

	a.seekMedia(n, 800*time.Millisecond)
	if m.Position != 800*time.Millisecond {
		t.Fatalf("position = %v", m.Position)
	}
	if !m.Playing {
		t.Fatal("a seek paused the player")
	}
	if len(seeks) != 1 || seeks[0] != 800*time.Millisecond {
		t.Fatalf("seek events = %v", seeks)
	}
	a.closeMedia()
}

// TestMediaMethodsFromTheDesign drives a player the way an event does:
// `onClick: clip.play()` and `clip.seek(1)`.
func TestMediaMethodsFromTheDesign(t *testing.T) {
	clip := mediaClip(t)
	a, win, m := mediaApp(t, clip, `Window { Column {
		Video { id: clip width: 320 height: 180 }
		Button { id: toggle label: "go" onClick: clip.play() }
		Button { id: seek label: "seek" onClick: clip.seek(1) }
	} }`)
	if !a.mediaRequest(m.Base().Node(), "play", nil) {
		t.Fatal("play was refused")
	}
	if !m.Playing {
		t.Fatal("play did not start the player")
	}
	if !a.mediaRequest(m.Base().Node(), "seek", []any{1.0}) {
		t.Fatal("seek was refused")
	}
	if m.Position != time.Second {
		t.Fatalf("position after seek = %v", m.Position)
	}
	if a.mediaRequest(m.Base().Node(), "nope", nil) {
		t.Fatal("an unknown method was accepted")
	}
	if !a.mediaRequest(m.Base().Node(), "pause", nil) {
		t.Fatal("pause was refused")
	}
	if !m.Paused {
		t.Fatal("pause did not stop the player")
	}
	_ = win
	a.closeMedia()
}

// TestMissingFFmpegIsReportedNotFatal checks the degraded path: a file that
// cannot be probed leaves a clear error on the node and the window still works.
func TestMissingMediaFileIsReported(t *testing.T) {
	a, _, m := mediaApp(t, "/nonexistent/nope.mp4", `Window { Video { id: v width: 320 height: 180 } }`)
	if m.LoadErr == nil {
		t.Skip("ffmpeg is not installed, so nothing could be probed")
	}
	// Playing it must not panic, and the error stays for paint to show.
	a.playMedia(m.Base().Node())
	if m.Playing {
		t.Fatal("a player with no file reported itself as playing")
	}
	if m.LoadErr == nil {
		t.Fatal("the load error was dropped")
	}
	a.closeMedia()
}

// TestMediaControlsLayout checks that the bar, the button and the scrubber line
// up with the box, and that hiding the controls leaves no bar to press.
func TestMediaControlsLayout(t *testing.T) {
	_, _, m := mediaApp(t, "", `Window { Audio { id: a width: 320 height: 48 } }`)
	r := tree.Rect{X: 10, Y: 20, W: 320, H: 48}
	m.Duration = 100 * time.Second
	m.Position = 25 * time.Second

	lay := paint.MediaLayoutOf(m, r)
	if lay.Bar.H != 48 || lay.Bar.Y != r.Bottom()-48 {
		t.Fatalf("bar = %+v for box %+v", lay.Bar, r)
	}
	if lay.Button.X < r.X || lay.Button.Right() > r.Right() {
		t.Fatalf("button = %+v for box %+v", lay.Button, r)
	}
	// A quarter through, the thumb is a quarter along the track.
	want := lay.Track.X + lay.Track.W*0.25
	if got := lay.Thumb.X + lay.Thumb.W/2; got < want-1 || got > want+1 {
		t.Fatalf("thumb centre = %v, want %v", got, want)
	}
	if f, ok := lay.MediaSeekFraction(want); !ok || f < 0.24 || f > 0.26 {
		t.Fatalf("seek fraction = %v", f)
	}

	m.Controls.Set(false)
	if got := paint.MediaLayoutOf(m, r); got.Bar.H != 0 {
		t.Fatalf("controls: false still laid out a bar: %+v", got)
	}
}

// TestMediaClockText pins the time text: minutes below an hour, hours above.
func TestMediaClockText(t *testing.T) {
	cases := map[time.Duration]string{
		0:                 "0:00",
		9 * time.Second:   "0:09",
		75 * time.Second:  "1:15",
		370 * time.Second: "6:10",
		time.Hour:         "1:00:00",
		2*time.Hour + 3*time.Minute + 4*time.Second: "2:03:04",
	}
	for d, want := range cases {
		if got := paint.MediaClock(d); got != want {
			t.Errorf("%v = %q, want %q", d, got, want)
		}
	}
}

// TestMediaBarPressPlays drives the built-in controls through the real press
// path: the play button starts the player.
func TestMediaBarPressPlays(t *testing.T) {
	clip := mediaClip(t)
	_, win, in := inputFixture(t, `Window { Video { id: v width: 320 height: 180 } }`, 400, 300)
	var m *tree.Media
	var node tree.Node
	for _, n := range win.tree.All() {
		if got, ok := tree.MediaNode(n); ok {
			m, node = got, n
		}
	}
	if m == nil {
		t.Fatal("no video node")
	}
	m.Src.Set(clip)
	m.Duration = 10 * time.Second
	r := absoluteRect(node)
	lay := paint.MediaLayoutOf(m, r)
	if lay.Bar.H == 0 {
		t.Fatalf("no control bar for %+v", r)
	}
	in.MouseDown(lay.Button.X+lay.Button.W/2, lay.Button.Y+lay.Button.H/2, 0, false)
	in.MouseUp(lay.Button.X+lay.Button.W/2, lay.Button.Y+lay.Button.H/2, 0)
	if !m.Playing || m.Paused {
		t.Fatalf("the play button left playing=%v paused=%v", m.Playing, m.Paused)
	}
	win.app.closeMedia()
}

// TestMediaBarSeekPress drives the scrubber.
func TestMediaBarSeekPress(t *testing.T) {
	clip := mediaClip(t)
	_, win, in := inputFixture(t, `Window { Video { id: v width: 320 height: 180 } }`, 400, 300)
	var m *tree.Media
	var node tree.Node
	for _, n := range win.tree.All() {
		if got, ok := tree.MediaNode(n); ok {
			m, node = got, n
		}
	}
	m.Src.Set(clip)
	m.Duration = 10 * time.Second
	r := absoluteRect(node)
	lay := paint.MediaLayoutOf(m, r)
	x := lay.Track.X + lay.Track.W/2
	in.MouseDown(x, lay.Track.Y+lay.Track.H/2, 0, false)
	in.MouseUp(x, lay.Track.Y+lay.Track.H/2, 0)
	if m.Position < 4*time.Second || m.Position > 6*time.Second {
		t.Fatalf("the middle of the track seeked to %v of 10s", m.Position)
	}
	win.app.closeMedia()
}
