//go:build !js && !wasip1

package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSDLAssetsArePinned(t *testing.T) {
	for key, a := range sdlAssets {
		if !strings.HasPrefix(a.url, "https://") || a.name == "" || a.member == "" {
			t.Fatalf("%s asset = %+v, want a URL, name and member", key, a)
		}
		if len(a.sha256) != 64 {
			t.Fatalf("%s sha256 = %q", key, a.sha256)
		}
		if strings.HasPrefix(key, "darwin/") && a.mountMember == "" {
			t.Fatalf("%s needs the mounted member path", key)
		}
	}
	for _, key := range []string{"windows/amd64", "windows/arm64", "windows/386", "darwin/amd64", "darwin/arm64"} {
		if _, ok := sdlAssets[key]; !ok {
			t.Fatalf("no SDL3 asset for %s", key)
		}
	}
	if _, ok := sdlAssets["linux/amd64"]; ok {
		t.Fatal("SDL3 ships no Linux binary; a download entry would point at nothing")
	}
}

func TestExtractSDLZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "sdl.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("SDL3.dll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("dll")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := extractZipMember(archive, "SDL3.dll", dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(got); err != nil || string(b) != "dll" {
		t.Fatalf("extracted = %q, %v", b, err)
	}
	if _, err := extractZipMember(archive, "missing.dll", dir); err == nil {
		t.Fatal("a missing member was extracted")
	}
}

func TestResolvePackSDL(t *testing.T) {
	sdl := filepath.Join(t.TempDir(), "libSDL3.so.0")
	if err := os.WriteFile(sdl, []byte("so"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolvePackSDL(packOptions{OS: "linux", Arch: runtime.GOARCH, SDL: sdl}, io.Discard); err != nil || got != sdl {
		t.Fatalf("--sdl = %q, %v", got, err)
	}
	if got, err := resolvePackSDL(packOptions{OS: "linux", Arch: runtime.GOARCH, NoSDL: true}, io.Discard); err != nil || got != "" {
		t.Fatalf("--no-sdl = %q, %v", got, err)
	}
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	if _, err := resolvePackSDL(packOptions{OS: "linux", Arch: other}, io.Discard); err == nil || !strings.Contains(err.Error(), "no official Linux binary") {
		t.Fatalf("cross Linux error = %v", err)
	}
	if _, err := resolvePackSDL(packOptions{OS: "windows", Arch: "plan9"}, io.Discard); err == nil || !strings.Contains(err.Error(), "no SDL3 binary") {
		t.Fatalf("unknown target error = %v", err)
	}
}

func TestPackSDLFileNames(t *testing.T) {
	cases := map[string]string{
		"linux":   "libSDL3.so.0",
		"darwin":  "libSDL3.0.dylib",
		"windows": "SDL3.dll",
	}
	for goos, want := range cases {
		if got := packSDLFile(goos); got != want {
			t.Fatalf("packSDLFile(%s) = %q, want %q", goos, got, want)
		}
	}
}
