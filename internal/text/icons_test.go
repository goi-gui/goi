package text

import (
	"strings"
	"testing"
)

// TestIconNamesReadsTheFont guards the ligature walk: the names come from the
// embedded woff, so a font or parser change that breaks the walk fails here
// instead of silently emptying the editor's completion list.
func TestIconNamesReadsTheFont(t *testing.T) {
	names := IconNames()
	if len(names) < 1000 {
		t.Fatalf("found %d icon names, expected the font's whole set", len(names))
	}
	have := make(map[string]bool, len(names))
	for _, n := range names {
		have[n] = true
	}
	for _, want := range []string{"home", "add", "arrow_back", "favorite", "delete"} {
		if !have[want] {
			t.Errorf("missing %q", want)
		}
	}
}

// TestIconNamesAreIdentifiers documents why completion can insert a bare name:
// every ligature is lowercase ASCII, digits and underscores.
func TestIconNamesAreIdentifiers(t *testing.T) {
	for _, n := range IconNames() {
		if n == "" || strings.Trim(n, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
			t.Fatalf("%q is not a bare identifier", n)
		}
	}
}
