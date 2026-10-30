// Package hotreload watches .goi source directories and reports when a file
// changes.
//
// A design is usually split across files, so the watcher covers a set of
// directories rather than one file: any write anywhere in them means "reload
// the whole design".
package hotreload

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Reloader watches directories and calls onChange after a short debounce.
// onChange runs on the watcher goroutine; callers must hand the work to the UI
// goroutine.
type Reloader struct {
	debounce time.Duration
	onChange func(path string) // the file that triggered the reload
	w        *fsnotify.Watcher
	dirs     map[string]bool

	mu     sync.Mutex
	timer  *time.Timer
	closed bool
}

// Watch starts watching dirs. Only .goi files trigger a reload. Directories that
// do not exist are skipped, so a design that has not created a part directory
// yet still loads.
func Watch(dirs []string, onChange func(path string)) (*Reloader, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	r := &Reloader{debounce: 80 * time.Millisecond, onChange: onChange, w: w, dirs: map[string]bool{}}
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		if dir == "" || r.dirs[dir] {
			continue
		}
		if err := w.Add(dir); err != nil {
			// A missing directory is not fatal: the design may not use one.
			continue
		}
		r.dirs[dir] = true
	}
	if len(r.dirs) == 0 {
		w.Close()
		return nil, nil
	}
	go r.loop()
	return r, nil
}

// Dirs returns the watched directories.
func (r *Reloader) Dirs() []string {
	out := make([]string, 0, len(r.dirs))
	for d := range r.dirs {
		out = append(out, d)
	}
	return out
}

// Close stops watching. Safe to call more than once.
func (r *Reloader) Close() {
	if r == nil {
		return
	}
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
			if !r.relevant(ev) {
				continue
			}
			r.schedule(ev.Name)
		case _, ok := <-r.w.Errors:
			if !ok {
				return
			}
		}
	}
}

// relevant filters editor noise: only .goi files that were written, created or
// renamed in.
func (r *Reloader) relevant(ev fsnotify.Event) bool {
	if filepath.Ext(ev.Name) != ".goi" {
		return false
	}
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
		return false
	}
	// The event may come from a subdirectory the watcher has not added yet.
	dir := filepath.Dir(filepath.Clean(ev.Name))
	r.mu.Lock()
	watched := r.dirs[dir]
	r.mu.Unlock()
	return watched
}

// schedule debounces bursts of writes (editors often emit several).
func (r *Reloader) schedule(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = time.AfterFunc(r.debounce, func() { r.fire(path) })
}

func (r *Reloader) fire(path string) {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return
	}
	r.onChange(path)
}
