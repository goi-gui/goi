package media

import (
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"
)

func fixture(t *testing.T) string {
	t.Helper()
	if !Available() {
		t.Skip("ffmpeg is not installed")
	}
	dir := t.TempDir()
	path := dir + "/clip.mp4"
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=160x90:rate=10:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build the fixture: %v\n%s", err, out)
	}
	return path
}

func TestProbeAndDecode(t *testing.T) {
	path := fixture(t)
	info, err := Probe(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 160 || info.Height != 90 {
		t.Fatalf("size = %dx%d", info.Width, info.Height)
	}
	if !info.HasVideo || !info.HasAudio {
		t.Fatalf("streams = %+v", info)
	}
	if info.Duration < 900*time.Millisecond || info.Duration > 1300*time.Millisecond {
		t.Fatalf("duration = %v", info.Duration)
	}

	v, err := OpenVideo(path, 80, 44, 0, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	frame := make([]byte, v.FrameBytes)
	n, err := v.Read(frame)
	if err != nil {
		t.Fatalf("first frame: %v", err)
	}
	if n != v.FrameBytes {
		t.Fatalf("read %d bytes, want %d", n, v.FrameBytes)
	}
	// testsrc is colourful, so a black frame means the pipe or the scaler is
	// wrong.
	bright := 0
	for i := 0; i < len(frame); i += 4 {
		if frame[i] > 40 || frame[i+1] > 40 || frame[i+2] > 40 {
			bright++
		}
	}
	if bright == 0 {
		t.Fatal("the first frame is entirely black")
	}
	count := 1
	for {
		if _, err := v.Read(frame); err != nil {
			break
		}
		count++
	}
	if count < 5 {
		t.Fatalf("decoded %d frames of a one second, 10 fps clip", count)
	}
	_ = os.Remove(path)
}

// TestLiveVideoIsPacedNotBursty pins the live input pacing. An HLS playlist
// publishes a segment at a time, so without "-re" ffmpeg decodes the whole
// backlog as fast as the network allows and fills the byte-capped queue in a
// fraction of a second; the picture then freezes until the next refresh while
// the sound keeps real time. A file must not be paced: the buffer is what keeps
// a recorded film smooth.
func TestLiveVideoIsPacedNotBursty(t *testing.T) {
	src := "https://example.com/live.m3u8"
	args := videoArgs(src, 640, 360, 0, false, true, true)
	input, paced := slices.Index(args, "-i"), slices.Index(args, "-re")
	if paced < 0 || input < 0 || paced > input {
		t.Fatalf("-re is not an input option for a live feed: %v", args)
	}
	if start := slices.Index(args, "-live_start_index"); start < 0 || start > input {
		t.Fatalf("-live_start_index is not an input option: %v", args)
	}
	// A live source without HLS (behind dropped) is still paced, just not at a
	// segment index.
	plain := videoArgs("rtsp://example.com/live", 640, 360, 0, false, false, true)
	if slices.Index(plain, "-re") < 0 {
		t.Fatalf("a live non-HLS source was not paced: %v", plain)
	}
	file := videoArgs("/tmp/clip.mp4", 640, 360, 0, false, false, false)
	if slices.Index(file, "-re") >= 0 {
		t.Fatalf("a recorded file was paced: %v", file)
	}
}

func TestPCMDecodes(t *testing.T) {
	path := fixture(t)
	p, err := OpenPCM(path, 48000, 2, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	buf := make([]byte, 4096)
	total := 0
	for {
		n, err := p.Read(buf)
		total += n
		if err != nil {
			break
		}
	}
	// One second of stereo 48k s16 is 192000 bytes; allow the encoder's padding.
	if total < 100000 {
		t.Fatalf("decoded %d bytes of PCM", total)
	}
}

func TestMissingFileReportsTheReason(t *testing.T) {
	_, err := Probe("/nonexistent/nope.mp4")
	if err == nil {
		t.Fatal("probing a missing file reported no error")
	}
}

// TestLiveDetection checks the one rule that decides a feed is live: a network
// source whose container reports no length.
func TestLiveDetection(t *testing.T) {
	for _, src := range []string{
		"https://example.com/live.m3u8",
		"http://example.com/stream.flv",
		"rtmp://example.com/live/key",
		"rtsp://camera.local/stream",
		"srt://host:9000?mode=caller",
		"udp://239.0.0.1:1234",
	} {
		if !IsURL(src) {
			t.Errorf("IsURL(%q) = false", src)
		}
		if !(Info{}).IsLive(src) {
			t.Errorf("a stream with no duration is not live: %q", src)
		}
		// A stream that reports a length is a recording, not a live feed.
		if (Info{Duration: time.Minute}).IsLive(src) {
			t.Errorf("%q with a duration is live", src)
		}
	}
	for _, src := range []string{
		"clip.mp4", "./media/clip.mp4", "/tmp/clip.mp4", `C:\media\clip.mp4`,
		"file:///tmp/clip.mp4", "goi://media/clip.mp4",
	} {
		if IsURL(src) {
			t.Errorf("IsURL(%q) = true", src)
		}
		if (Info{}).IsLive(src) {
			t.Errorf("a file with no duration is live: %q", src)
		}
	}
}
