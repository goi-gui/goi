package ui

import (
	"path/filepath"
	"testing"
)

// TestDialogLocation covers the start path: a directory, a file name, or both.
func TestDialogLocation(t *testing.T) {
	cases := []struct{ dir, name, want string }{
		{"", "", ""},
		{"/tmp", "", "/tmp"},
		{"", "design.goi", "design.goi"},
		{"/tmp", "design.goi", filepath.Join("/tmp", "design.goi")},
	}
	for _, c := range cases {
		o := dialogOptions{dir: c.dir, filename: c.name}
		if got := o.location(); got != c.want {
			t.Errorf("dir %q name %q = %q, want %q", c.dir, c.name, got, c.want)
		}
	}
}
