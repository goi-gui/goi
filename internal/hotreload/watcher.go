// Package hotreload watches a .ui file and hands new contents to the app.
//
// It watches the containing directory rather than the file, because many
// editors save by writing a temporary file and renaming it over the original,
// which would silently break a file watch.
package hotreload

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Reloader watches one file and calls onChange with its bytes after a short
// debounce. onChange runs on the watcher goroutine; callers must hand the work
// to the UI goroutine.
type Reloader struct {
	path     string
	debounce time.Duration
	onChange func([]byte, string) // contents, error text
	w        *fsnotify.Watcher

	mu     sync.Mutex
	timer  *time.Timer
	closed bool
}

// Path returns the watched file path.
func (r *Reloader) Path() string { return r.path }

// Watch starts watching path. The initial contents are not reported.
func Watch(path string, onChange func(src []byte, errText string)) (*Reloader, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// Watch the directory so atomic renames keep working.
	if err := w.Add(filepath.Dir(path)); err != nil {
		w.Close()
		return nil, err
	}
	r := &Reloader{path: path, debounce: 80 * time.Millisecond, onChange: onChange, w: w}
	go r.loop()
	return r, nil
}

// Close stops watching. Safe to call more than once.
func (r *Reloader) Close() {
	r.mu.Lock()
	r.closed = true
	if r.timer != nil {
		r.timer.Stop()
	}
	r.mu.Unlock()
	r.w.Close()
}

func (r *Reloader) loop() {
	for {
		select {
		case ev, ok := <-r.w.Events:
			if !ok {
				return
			}
			// Match the file by name: the watch is on the directory.
			if filepath.Clean(ev.Name) != filepath.Clean(r.path) {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			r.schedule()
		case _, ok := <-r.w.Errors:
			if !ok {
				return
			}
		}
	}
}

// schedule debounces bursts of writes (editors often emit several).
func (r *Reloader) schedule() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = time.AfterFunc(r.debounce, r.fire)
}

func (r *Reloader) fire() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	src, err := os.ReadFile(r.path)
	if err != nil {
		r.onChange(nil, err.Error())
		return
	}
	r.onChange(src, "")
}
