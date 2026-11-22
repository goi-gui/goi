package ui

import (
	"bytes"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// liveAssets is a design container that overlays files pushed by a development
// server on top of the embedded build. Reads fall through to the build for
// everything the snapshot does not carry, so images and fonts keep working.
//
// A file the snapshot drops is hidden even when the build still embeds it, which
// is what makes a deleted fragment or catalog disappear without a new wasm.
type liveAssets struct {
	mu    sync.RWMutex
	base  fs.FS
	files map[string][]byte
	gone  map[string]bool
}

func newLiveAssets(base fs.FS) *liveAssets {
	return &liveAssets{base: base, files: map[string][]byte{}, gone: map[string]bool{}}
}

// update replaces the snapshot and reports every path whose content differs from
// what this container served before. It runs on the UI goroutine.
func (l *liveAssets) update(next map[string][]byte) []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	files := make(map[string][]byte, len(next))
	changed := make([]string, 0, len(next))
	for name, body := range next {
		files[name] = body
		if old, ok := l.files[name]; ok {
			if !bytes.Equal(old, body) {
				changed = append(changed, name)
			}
			continue
		}
		// The first snapshot usually matches the build: connecting is not an
		// edit, so a page that starts current does not rebuild its tree.
		if base, err := fs.ReadFile(l.base, name); err == nil && bytes.Equal(base, body) {
			continue
		}
		changed = append(changed, name)
	}
	for name := range l.files {
		if _, ok := next[name]; ok {
			continue
		}
		changed = append(changed, name)
		if _, err := fs.Stat(l.base, name); err == nil {
			l.gone[name] = true
		}
	}
	for name := range next {
		delete(l.gone, name)
	}
	l.files = files
	sort.Strings(changed)
	return changed
}

// Open implements fs.FS.
func (l *liveAssets) Open(name string) (fs.File, error) {
	key := cleanName(name)
	l.mu.RLock()
	body, live := l.files[key]
	gone := l.gone[key]
	l.mu.RUnlock()
	if gone {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	if live {
		return &memFile{name: path.Base(key), reader: bytes.NewReader(body)}, nil
	}
	return l.base.Open(name)
}

// ReadDir implements fs.ReadDirFS so i18n.Load sees catalogs added or removed by
// the snapshot, not only the ones the build embedded.
func (l *liveAssets) ReadDir(name string) ([]fs.DirEntry, error) {
	dir := cleanName(name)
	l.mu.RLock()
	files, gone := l.files, l.gone
	l.mu.RUnlock()

	var out []fs.DirEntry
	seen := map[string]bool{}
	entries, err := fs.ReadDir(l.base, name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		full := path.Join(dir, e.Name())
		if gone[full] {
			continue
		}
		if _, ok := files[full]; ok {
			continue
		}
		out = append(out, e)
		seen[e.Name()] = true
	}
	for full, body := range files {
		if dirOf(full) != dir {
			continue
		}
		name := path.Base(full)
		if seen[name] {
			continue
		}
		out = append(out, fs.FileInfoToDirEntry(memInfo{name: name, size: int64(len(body))}))
		seen[name] = true
	}
	// A directory that only the snapshot has still has to show up, or a new
	// catalog folder would be invisible to i18n.Load.
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	for full := range files {
		rest, ok := strings.CutPrefix(full, prefix)
		if !ok {
			continue
		}
		i := strings.IndexByte(rest, '/')
		if i <= 0 {
			continue
		}
		name := rest[:i]
		if seen[name] {
			continue
		}
		out = append(out, fs.FileInfoToDirEntry(memInfo{name: name, dir: true}))
		seen[name] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// dirOf is path.Dir with the root as "", which is how cleanName spells it.
func dirOf(name string) string {
	if d := path.Dir(name); d != "." {
		return d
	}
	return ""
}

// memFile is an in-memory snapshot entry.
type memFile struct {
	name   string
	reader *bytes.Reader
}

func (f *memFile) Read(p []byte) (int, error) { return f.reader.Read(p) }
func (f *memFile) Close() error               { return nil }
func (f *memFile) Stat() (fs.FileInfo, error) {
	return memInfo{name: f.name, size: int64(f.reader.Size())}, nil
}

type memInfo struct {
	name string
	size int64
	dir  bool
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return i.size }
func (i memInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.dir }
func (i memInfo) Sys() any           { return nil }
