//go:build !js && !wasip1

package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPackDarwin(t *testing.T) {
	dir := buildProjectDir(t, "name: myapp\nid: com.example.myapp\n")
	writePackIcon(t, filepath.Join(dir, "assets", "icon.png"), 512, 512)

	goRun := func(_ string, args []string, _, _ io.Writer, _ []string) error {
		out := args[len(args)-2]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, []byte("mach-o"), 0o755)
	}
	sdl := fakeSDL(t, "darwin", "amd64")
	var signed string
	runTool := func(path string, args []string, _ []string, _, _ io.Writer) error {
		signed = path + " " + strings.Join(args, " ")
		return nil
	}
	out, err := packProject(dir, packOptions{OS: "darwin", Arch: "amd64", SDL: sdl}, goRun, runTool, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "dist", "darwin-amd64", "myapp.app")
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	exe := filepath.Join(out, "Contents", "MacOS", "myapp")
	if b, err := os.ReadFile(exe); err != nil || string(b) != "mach-o" {
		t.Fatalf("executable = %q, %v", b, err)
	}
	plist, err := os.ReadFile(filepath.Join(out, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>CFBundleIdentifier</key>",
		"<string>com.example.myapp</string>",
		"<key>CFBundleExecutable</key>",
		"<key>CFBundleIconFile</key>",
		"<string>APPL</string>",
		"<key>NSHighResolutionCapable</key>",
	} {
		if !strings.Contains(string(plist), want) {
			t.Fatalf("Info.plist lacks %q:\n%s", want, plist)
		}
	}
	wantSDL, err := os.ReadFile(sdl)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "Contents", "MacOS", "libSDL3.0.dylib")); err != nil || !bytes.Equal(b, wantSDL) {
		t.Fatalf("bundled SDL3 = %d bytes, %v", len(b), err)
	}
	license, err := os.ReadFile(filepath.Join(out, "Contents", "Resources", "SDL3-LICENSE.txt"))
	if err != nil || !strings.Contains(string(license), "Permission is granted") {
		t.Fatalf("SDL3 license = %q, %v", license, err)
	}
	icns, err := os.ReadFile(filepath.Join(out, "Contents", "Resources", "com.example.myapp.icns"))
	if err != nil {
		t.Fatal(err)
	}
	checkICNS(t, icns)

	if runtime.GOOS == "darwin" {
		if !strings.HasPrefix(signed, "codesign ") || !strings.Contains(signed, want) {
			t.Fatalf("codesign = %q, want the bundle signed", signed)
		}
	} else if signed != "" {
		t.Fatalf("codesign ran on %s: %s", runtime.GOOS, signed)
	}
}

func checkICNS(t *testing.T, b []byte) {
	t.Helper()
	if len(b) < 8 || string(b[:4]) != "icns" {
		t.Fatalf("not an icns container: % x", b[:min(len(b), 8)])
	}
	if total := int(binary.BigEndian.Uint32(b[4:8])); total != len(b) {
		t.Fatalf("icns length = %d, file has %d bytes", total, len(b))
	}
	entries := 0
	for off := 8; off < len(b); {
		if off+8 > len(b) {
			t.Fatalf("truncated icns entry at %d", off)
		}
		size := int(binary.BigEndian.Uint32(b[off+4 : off+8]))
		if size < 8 || off+size > len(b) {
			t.Fatalf("icns entry size = %d at %d", size, off)
		}
		entries++
		off += size
	}
	if entries == 0 {
		t.Fatal("icns has no representations")
	}
}
