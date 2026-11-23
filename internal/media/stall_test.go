package media

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveReadTimesOut pins the stall bound: a decoder pipe that produces
// nothing must fail its read instead of blocking the pump, so the player can
// reopen it. Data that is already waiting still reads normally.
func TestLiveReadTimesOut(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	v := &Video{out: r, FrameBytes: 4, stderr: &strings.Builder{}, readTimeout: 100 * time.Millisecond}
	start := time.Now()
	if _, err := v.Read(make([]byte, 4)); err == nil {
		t.Fatal("an empty pipe read did not time out")
	} else if !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("error = %v, want a stalled decoder", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("read took %v, want about the 100ms bound", d)
	}

	if _, err := w.Write([]byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if n, err := v.Read(make([]byte, 4)); err != nil || n != 4 {
		t.Fatalf("read with data = %d, %v", n, err)
	}
}
