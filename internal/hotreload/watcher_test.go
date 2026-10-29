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
	ch := make(chan string, 8)
	r, err := Watch([]string{dir}, func(p string) { ch <- p })
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("no reloader for an existing directory")
	}
	defer r.Close()

	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if filepath.Base(got) != "app.ui" {
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
	r, err := Watch([]string{dir}, func(p string) { ch <- p })
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
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("rename was not detected")
	}
}

// TestWatchReportsFragmentChanges is the point of watching directories: editing
// a part must reload the design that uses it.
func TestWatchReportsFragmentChanges(t *testing.T) {
	dir := t.TempDir()
	parts := filepath.Join(dir, "parts")
	if err := os.Mkdir(parts, 0o755); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(parts, "Sidebar.ui")
	if err := os.WriteFile(part, []byte("Column {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan string, 8)
	r, err := Watch([]string{dir, parts}, func(p string) { ch <- p })
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if err := os.WriteFile(part, []byte("Row {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if filepath.Base(got) != "Sidebar.ui" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fragment change was not reported")
	}
}

func TestWatchIgnoresNonUIFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.ui"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan string, 8)
	r, err := Watch([]string{dir}, func(p string) { ch <- p })
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("nope"), 0o644); err != nil {
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
	r, err := Watch([]string{dir}, func(string) { count.Add(1) })
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

// TestWatchMissingDirectory returns nil rather than an error, so a design that
// does not use a parts directory still loads.
func TestWatchMissingDirectory(t *testing.T) {
	r, err := Watch([]string{filepath.Join(t.TempDir(), "nope")}, func(string) {})
	if err != nil {
		t.Fatalf("missing dir reported as %v", err)
	}
	if r != nil {
		defer r.Close()
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.ui"), []byte("x"), 0o644)
	r, err := Watch([]string{dir}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	r.Close()
}
