package lsp

import (
	"io"
	"io/fs"
	"path"
	"time"
)

// memFile is one in-memory source file, so a buffer can stand in for the file
// on disk while a design is validated.
type memFile struct {
	name string
	data []byte
	off  int
	info fs.FileInfo
}

func (m *memFile) Stat() (fs.FileInfo, error) {
	if m.info == nil {
		m.info = memInfo{name: path.Base(m.name), size: int64(len(m.data))}
	}
	return m.info, nil
}

func (m *memFile) Read(p []byte) (int, error) {
	if m.off >= len(m.data) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.off:])
	m.off += n
	return n, nil
}

func (m *memFile) Close() error { return nil }

// memInfo is the FileInfo for a memory file.
type memInfo struct {
	name string
	size int64
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return i.size }
func (i memInfo) Mode() fs.FileMode {
	return 0o444
}
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }
