//go:build !js

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSDLLibraryPathHonoursOverride pins the custom-prefix escape hatch.
func TestSDLLibraryPathHonoursOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "libSDL3-test.dylib")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOI_SDL3_PATH", path)
	if got := sdlLibraryPath(); got != path {
		t.Errorf("sdlLibraryPath = %q, want %q", got, path)
	}
}

// TestSDLLibraryPathIgnoresMissingOverride keeps a stale environment variable
// from breaking a working install: a missing file falls back to the search.
func TestSDLLibraryPathIgnoresMissingOverride(t *testing.T) {
	t.Setenv("GOI_SDL3_PATH", filepath.Join(t.TempDir(), "missing.dylib"))
	if got := sdlLibraryPath(); got == "" {
		t.Fatal("sdlLibraryPath returned an empty path")
	}
}
