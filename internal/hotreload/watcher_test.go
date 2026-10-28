package hotreload

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls until cond is true or the deadline passes.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestWatchReportsWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.ui")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu = make(chan string, 8)
	r, err := Watch(path, func(src []byte, errText string) {
		if errText != "" {
			mu <- "error:" + errText
			return
		}
		mu <- string(src)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-mu:
		if got != "second" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reload reported")
	}
}

func TestWatchSurvivesAtomicRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.ui")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan string, 8)
	r, err := Watch(path, func(src []byte, errText string) { ch <- string(src) })
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// Editors that save by rename: write a temp file, then rename over.
	tmp := filepath.Join(dir, "app.ui.tmp")
	if err := os.WriteFile(tmp, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if got != "two" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rename was not detected")
	}
}

func TestWatchIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.ui")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan string, 8)
	r, err := Watch(path, func(src []byte, errText string) { ch <- string(src) })
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if err := os.WriteFile(filepath.Join(dir, "other.ui"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		t.Fatalf("reported an unrelated file: %q", got)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWatchDebouncesBursts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.ui")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var count atomic.Int32
	r, err := Watch(path, func(src []byte, errText string) {
		count.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i := 0; i < 20; i++ {
		if err := os.WriteFile(path, []byte("y"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, 2*time.Second, func() bool { return count.Load() > 0 })
	time.Sleep(250 * time.Millisecond)
	if n := count.Load(); n > 5 {
		t.Fatalf("20 rapid writes produced %d reloads; debounce is not working", n)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.ui")
	os.WriteFile(path, []byte("x"), 0o644)
	r, err := Watch(path, func([]byte, string) {})
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	r.Close()
}
