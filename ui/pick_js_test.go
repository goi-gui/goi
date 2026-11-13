//go:build js

package ui

import "testing"

// TestPickAccept turns a design's filters into the picker's accept attribute: a
// wildcard becomes an extension, and anything else is passed through.
func TestPickAccept(t *testing.T) {
	got := pickAccept([]FileFilter{
		{Name: "Images", Patterns: []string{"*.png", "*.jpg"}},
		{Name: "Anything", Patterns: []string{"image/*"}},
	})
	if want := ".png,.jpg,image/*"; got != want {
		t.Fatalf("pickAccept = %q, want %q", got, want)
	}
	if got := pickAccept(nil); got != "" {
		t.Fatalf("no filters = %q, want empty", got)
	}
}
