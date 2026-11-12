package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/media"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

// multiAudioClip is the fixture with two language-ish audio streams, which is
// what a film with dubs looks like to ffmpeg.
func multiAudioClip(t *testing.T) string {
	t.Helper()
	if !media.Available() {
		t.Skip("ffmpeg is not installed")
	}
	path := filepath.Join(t.TempDir(), "dub.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=160x90:rate=10:duration=1.5",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1.5",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=1.5",
		"-map", "0:v", "-map", "1:a", "-map", "2:a",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build the fixture: %v\n%s", err, out)
	}
	return path
}

// TestAudioTracksAreCountedAndSelected covers the whole path: the probe sees both
// streams, the method picks the second, and the running pump follows without
// restarting the picture.
func TestAudioTracksAreCountedAndSelected(t *testing.T) {
	clip := multiAudioClip(t)
	a, _, m := mediaApp(t, clip, `Window { Video { id: v width: 320 height: 180 autoplay: true } }`)
	node := m.Base().Node()
	if m.AudioTracks != 2 {
		t.Fatalf("probed %d audio tracks, want 2", m.AudioTracks)
	}
	if _, err := a.startPlayer(node); err != nil {
		t.Fatal(err)
	}
	defer a.closeMedia()
	p := a.playerFor(node)
	if p == nil {
		t.Fatal("no player")
	}
	if p.track != 0 {
		t.Fatalf("started on track %d", p.track)
	}
	m.Playing = true

	a.setMediaAudioTrack(node, 1)
	if got := int(m.AudioTrack.Get()); got != 1 {
		t.Fatalf("AudioTrack = %d, want 1", got)
	}
	if p.track != 1 {
		t.Fatalf("the pump still reads track %d", p.track)
	}
	// Out-of-range indices are clamped rather than silencing the player.
	a.setMediaAudioTrack(node, 9)
	if got := int(m.AudioTrack.Get()); got != 1 {
		t.Fatalf("AudioTrack = %d after clamping, want 1", got)
	}
	if err := p.err(); err != nil {
		t.Fatalf("the track switch failed: %v", err)
	}
}

// TestAudioTrackMethodInDesign checks that the design side accepts the call.
func TestAudioTrackMethodInDesign(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Video { id: v width: 320 height: 180 } }`, 400, 300)
	v := MustByID[*Video](win, "v")
	v.Src.Set("clip.mp4")
	v.AudioTracks = 2
	in.app.mediaRequest(v, "audioTrack", []any{float64(1)})
	if got := int(v.AudioTrack.Get()); got != 1 {
		t.Fatalf("audioTrack(1) left the track at %d", got)
	}
}

// TestSubtitlesFollowTheClock loads an SRT beside a video and checks that the
// caption appears at its cue and nowhere else.
func TestSubtitlesFollowTheClock(t *testing.T) {
	a, win, m := mediaApp(t, mediaClip(t), `Window { Video { id: v width: 640 height: 360 } }`)
	srt := filepath.Join(t.TempDir(), "clip.srt")
	doc := "1\n00:00:00,000 --> 00:00:01,000\nhello caption\n\n" +
		"2\n00:00:01,500 --> 00:00:02,500\nsecond line\n"
	if err := os.WriteFile(srt, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Subtitles.Set(srt)
	a.syncMedia()
	if len(m.Cues) != 2 {
		t.Fatalf("%d cues loaded (%v)", len(m.Cues), m.SubtitleErr)
	}

	caption := func(at time.Duration) string {
		m.Position = at
		l := &paint.List{}
		l.Build(win.tree, 640, 360)
		for _, op := range l.Ops {
			if op.Kind == paint.Text && strings.Contains(op.Text, "caption") {
				return op.Text
			}
			if op.Kind == paint.Text && strings.Contains(op.Text, "second line") {
				return op.Text
			}
		}
		return ""
	}
	if got := caption(500 * time.Millisecond); got != "hello caption" {
		t.Fatalf("at 0.5s painted %q", got)
	}
	if got := caption(1200 * time.Millisecond); got != "" {
		t.Fatalf("between cues painted %q", got)
	}
	if got := caption(2 * time.Second); got != "second line" {
		t.Fatalf("at 2s painted %q", got)
	}
}

// TestBadSubtitlesAreReported checks that a damaged file leaves the video
// playable and says why through SubtitleErr.
func TestBadSubtitlesAreReported(t *testing.T) {
	a, _, m := mediaApp(t, mediaClip(t), `Window { Video { id: v width: 320 height: 180 } }`)
	srt := filepath.Join(t.TempDir(), "broken.srt")
	if err := os.WriteFile(srt, []byte("not a subtitle file"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Subtitles.Set(srt)
	a.syncMedia()
	if m.SubtitleErr == nil {
		t.Fatal("a broken subtitle file loaded silently")
	}
	if len(m.Cues) != 0 {
		t.Fatalf("%d cues from a broken file", len(m.Cues))
	}
}

// TestControlsAutoHide is the bar's clock: it hides after three seconds without
// pointer movement and comes back on the next move.
func TestControlsAutoHide(t *testing.T) {
	a, win, in := inputFixture(t, `Window { Video { id: v width: 320 height: 180 } }`, 400, 300)
	v := MustByID[*Video](win, "v")
	v.Src.Set(mediaClip(t))
	if _, err := a.startPlayer(v); err != nil {
		t.Fatal(err)
	}
	defer a.closeMedia()
	v.Playing = true
	// No length, so the clip's own end does not stop the clock these ticks are
	// driving.
	v.Live, v.Duration = true, 0
	if !v.ControlsShown {
		t.Fatal("the bar did not start visible")
	}
	// Two seconds is not enough.
	for i := 0; i < 40; i++ {
		a.tickMedia(0.05)
	}
	if !v.ControlsShown {
		t.Fatal("the bar hid before its three seconds")
	}
	for i := 0; i < 25; i++ {
		a.tickMedia(0.05)
	}
	if v.ControlsShown {
		t.Fatal("the bar did not hide after three seconds")
	}
	if lay := paint.MediaLayoutOf(&v.Media, tree.Rect{W: 320, H: 180}); lay.Bar.H != 0 {
		t.Fatalf("a hidden bar still lays out: %+v", lay)
	}
	// A pointer move over the picture brings it back and restarts the clock.
	b := absoluteRect(v)
	in.MouseMove(b.X+10, b.Y+10)
	if !v.ControlsShown {
		t.Fatal("a pointer move did not bring the bar back")
	}
	if v.ControlsIdle != 0 {
		t.Fatalf("the idle clock = %v after a move", v.ControlsIdle)
	}
}

// TestHardwareDecodeFallsBack is the decode guarantee: asking for hardware never
// leaves a player blank. On a machine with a working decoder the frame comes
// from the GPU; on one without, the pump retries in software before the first
// frame and the picture still arrives.
func TestHardwareDecodeFallsBack(t *testing.T) {
	clip := mediaClip(t)
	a, _, m := mediaApp(t, clip, `Window { Video { id: v width: 320 height: 180 } }`)
	if _, err := a.startPlayer(m.Base().Node()); err != nil {
		t.Fatal(err)
	}
	defer a.closeMedia()
	p := a.playerFor(m.Base().Node())
	if p == nil {
		t.Fatal("no player")
	}
	if !p.hw {
		t.Fatal("hardware decoding was not attempted")
	}
	deadline := time.Now().Add(5 * time.Second)
	m.Live = true // no end, so the ticks below only drive the player
	for p.buffered() == 0 && p.err() == nil && time.Now().Before(deadline) {
		a.tickMedia(0.01)
		time.Sleep(10 * time.Millisecond)
	}
	if err := p.err(); err != nil {
		t.Fatalf("decoding failed: %v", err)
	}
	if p.buffered() == 0 {
		t.Fatal("no frame arrived, hardware or software")
	}
	// One frame of the loop publishes the mode the decoder used.
	a.tickMedia(0.01)
	// The reported mode matches the decoder that actually produced the frame.
	if m.Hardware != p.hw {
		t.Fatalf("Hardware = %v, decoder = %v", m.Hardware, p.hw)
	}
}

// TestPausedPlayerStillHidesItsBar checks the wake-up: a paused player schedules
// the frame that hides its bar instead of waiting for input.
func TestPausedPlayerStillHidesItsBar(t *testing.T) {
	a, _, m := mediaApp(t, mediaClip(t), `Window { Video { id: v width: 320 height: 180 } }`)
	if _, err := a.startPlayer(m.Base().Node()); err != nil {
		t.Fatal(err)
	}
	defer a.closeMedia()
	// The clip's duration would end the player mid-tick; this test is about the
	// bar's clock alone.
	m.Live, m.Duration = true, 0
	m.ControlsShown = true
	m.Playing = false
	_, wait := a.tickMedia(0.5)
	if wait <= 0 || wait > controlsHide {
		t.Fatalf("a paused player asked the loop to wait %v", wait)
	}
}

// TestTakeFrameSwapsInsteadOfCopying guards the per-frame copy: the decoded
// buffer becomes the canvas and the old one goes back to the pump, which is one
// pointer swap instead of a full-frame memcpy.
func TestTakeFrameSwapsInsteadOfCopying(t *testing.T) {
	clip := mediaClip(t)
	a, _, m := mediaApp(t, clip, `Window { Video { id: v width: 320 height: 180 } }`)
	v, _ := m.Base().Node().(*tree.Video)
	if _, err := a.startPlayer(m.Base().Node()); err != nil {
		t.Fatal(err)
	}
	defer a.closeMedia()
	p := a.playerFor(m.Base().Node())
	if p == nil {
		t.Fatal("no player")
	}
	deadline := time.Now().Add(5 * time.Second)
	for p.buffered() == 0 && p.err() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if p.buffered() == 0 {
		t.Fatalf("no decoded frame (err %v)", p.err())
	}
	canvas := v.Data.Pix
	if !p.takeFrame() {
		t.Fatal("takeFrame found nothing although the queue had a frame")
	}
	if &v.Data.Pix[0] == &canvas[0] {
		t.Fatal("the frame was copied into the old canvas instead of swapped")
	}
	if len(v.Data.Pix) != len(canvas) {
		t.Fatalf("the swapped canvas is %d bytes, want %d", len(v.Data.Pix), len(canvas))
	}
	// The old canvas went back to the pool instead of being dropped, so the
	// pump can refill it: a later frame still arrives and the pool keeps its
	// fixed size.
	if got := cap(p.free) + cap(p.frames); got != 2*cap(p.frames) {
		t.Fatalf("pool shrank to %d slots", got)
	}
	deadline = time.Now().Add(5 * time.Second)
	for p.buffered() == 0 && p.err() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if p.buffered() == 0 {
		t.Fatal("the pump did not refill after the swap")
	}
}
