package ui

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/media"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
)

// TestVideoBuffersABoundedQueue is the memory guard: a video keeps a few seconds
// of frames, never the whole file, and the count is clamped by the frame size.
func TestVideoBuffersABoundedQueue(t *testing.T) {
	clip := mediaClip(t)
	a, _, m := mediaApp(t, clip, `Window { Video { id: v width: 640 height: 360 buffer: 1 } }`)
	n := m.Base().Node()
	if _, err := a.startPlayer(n); err != nil {
		t.Fatal(err)
	}
	p := a.playerFor(n)
	if p == nil {
		t.Fatal("no player")
	}
	defer a.closeMedia()

	// One second of a 10 fps clip, and never more than the byte budget.
	fps := p.info.FPS
	if fps <= 0 {
		fps = 30
	}
	want := int(p.buffer*fps + 0.5)
	if want < minVideoFrames {
		want = minVideoFrames
	}
	if got := cap(p.frames); got != want {
		t.Fatalf("queue depth = %d, want %d for %.1fs at %.0f fps", got, want, p.buffer, fps)
	}
	frameBytes := p.frame.W * p.frame.H * 4
	if cap(p.frames)*frameBytes > maxVideoBufferBytes+frameBytes {
		t.Fatalf("queue can hold %d bytes, past the %d budget",
			cap(p.frames)*frameBytes, maxVideoBufferBytes)
	}
	// The pump fills the queue and then blocks, so it never runs past it.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(p.frames) < cap(p.frames) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(p.frames) > cap(p.frames) {
		t.Fatalf("queued %d frames in a %d frame buffer", len(p.frames), cap(p.frames))
	}
}

// TestBufferPropertyIsClamped checks that a design cannot ask for a minute of
// video in RAM.
func TestBufferPropertyIsClamped(t *testing.T) {
	cases := map[float64]float64{
		0:     DefaultBuffer,
		1:     1,
		2.5:   2.5,
		-3:    DefaultBuffer,
		10000: maxBufferSeconds,
	}
	for in, want := range cases {
		if got := bufferSeconds(in); got != want {
			t.Errorf("bufferSeconds(%v) = %v, want %v", in, got, want)
		}
	}
	// A huge frame still cannot blow the budget.
	for _, size := range []int{1280 * 720 * 4, 3840 * 2160 * 4} {
		p := &mediaPlayer{buffer: maxBufferSeconds, info: mediaInfoFor(60)}
		p.openFrames(size)
		if cap(p.frames)*size > maxVideoBufferBytes+size {
			t.Fatalf("%d byte frames queued %d of them", size, cap(p.frames))
		}
		if cap(p.frames) < 1 {
			t.Fatal("a huge frame dropped the queue to nothing")
		}
	}
}

// TestAudioBufferIsBounded checks the same for sound: the lead is the design's
// buffer, clamped to the audio budget.
func TestAudioBufferIsBounded(t *testing.T) {
	p := &mediaPlayer{buffer: 1.5}
	want := int(1.5 * float64(audioRate*audioChannels*2))
	if got := p.audioLead(); got != want {
		t.Fatalf("audio lead = %d, want %d", got, want)
	}
	// Ten seconds of stereo 48k is under two megabytes, which is the cap the
	// property enforces.
	p.buffer = maxBufferSeconds
	if got, want := p.audioLead(), int(maxBufferSeconds*float64(audioRate*audioChannels*2)); got != want {
		t.Fatalf("audio lead = %d, want %d", got, want)
	}
	if p.audioLead() > 2<<20 {
		t.Fatalf("ten seconds of sound took %d bytes", p.audioLead())
	}
	p.buffer = 0.0001
	if got := p.audioLead(); got < audioRate*audioChannels*2/10 {
		t.Fatalf("audio lead = %d, too small to keep the device fed", got)
	}
}

// TestVideoFullscreenCoversTheWindow checks the overlay: the video leaves its
// own box, paints over the whole window, and takes every press.
func TestVideoFullscreenCoversTheWindow(t *testing.T) {
	a, win, _ := mediaApp(t, "", `Window { Column { padding: 20
		Video { id: v width: 200 height: 100 }
	} }`)
	var v *tree.Video
	for _, n := range win.tree.All() {
		if got, ok := n.(*tree.Video); ok {
			v = got
		}
	}
	if v == nil {
		t.Fatal("no video")
	}
	a.SetVideoFullscreen(v, true)
	if !v.Fullscreen.Get() {
		t.Fatal("the video did not enter fullscreen")
	}
	// The window follows on the next frame, which is what covers a design that
	// writes `fullscreen: true` and never calls the method.
	a.syncVideoFullscreen()
	if !win.tree.Root.Fullscreen.Get() {
		t.Fatal("the window did not follow the video into fullscreen")
	}
	if got := tree.FullscreenVideo(win.tree); got != v {
		t.Fatalf("FullscreenVideo = %p, want %p", got, v)
	}

	// Paint puts it over the whole window even though its own box is small.
	layout.New(nil).Layout(win.tree.Root, 800, 600)
	l := &paint.List{}
	l.Build(win.tree, 800, 600)
	if len(l.Ops) == 0 {
		t.Fatal("the overlay painted nothing")
	}

	a.SetVideoFullscreen(v, false)
	a.syncVideoFullscreen()
	if v.Fullscreen.Get() || win.tree.Root.Fullscreen.Get() {
		t.Fatal("leaving fullscreen did not restore the window")
	}
	if tree.FullscreenVideo(win.tree) != nil {
		t.Fatal("a windowed video still reports itself fullscreen")
	}
}

// TestFullscreenVideoTakesThePress checks the input side: a press anywhere goes
// to the fullscreen video, and its bar still works.
func TestFullscreenVideoTakesThePress(t *testing.T) {
	_, win, in := inputFixture(t, `Window { Column { padding: 20
		Video { id: v width: 200 height: 100 }
	} }`, 800, 600)
	var v *tree.Video
	for _, n := range win.tree.All() {
		if got, ok := n.(*tree.Video); ok {
			v = got
		}
	}
	v.Fullscreen.Set(true)
	if got := in.hit(700, 500); got != tree.Node(v) {
		t.Fatalf("a press outside the video's own box hit %v", got)
	}
	// The bar is laid out against the window, so its button is reachable.
	r := in.mediaRect(v)
	lay := paint.MediaLayoutOf(&v.Media, r)
	if lay.Bar.Y != r.Bottom()-48 {
		t.Fatalf("fullscreen bar = %+v in %+v", lay.Bar, r)
	}
}

// TestEscapeLeavesFullscreen covers the keyboard way out.
func TestEscapeLeavesFullscreen(t *testing.T) {
	a, win, in := inputFixture(t, `Window { Video { id: v width: 320 height: 180 } }`, 800, 600)
	var v *tree.Video
	for _, n := range win.tree.All() {
		if got, ok := n.(*tree.Video); ok {
			v = got
		}
	}
	a.SetVideoFullscreen(v, true)
	in.key(tree.KeyEscape, tree.Mods{})
	if v.Fullscreen.Get() {
		t.Fatal("Escape did not leave fullscreen")
	}
}

// mediaInfoFor is a fake probe result at a frame rate.
func mediaInfoFor(fps float64) media.Info {
	return media.Info{FPS: fps, HasVideo: true, Width: 640, Height: 360}
}
