//go:build !js && !wasip1

package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
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
	sdl := fakeSDL(t, "linux", runtime.GOARCH)
	if got, err := resolvePackSDL(packOptions{OS: "linux", Arch: runtime.GOARCH, SDL: sdl}, io.Discard); err != nil || got != sdl {
		t.Fatalf("--sdl = %q, %v", got, err)
	}
	if got, err := resolvePackSDL(packOptions{OS: "linux", Arch: runtime.GOARCH, NoSDL: true}, io.Discard); err != nil || got != "" {
		t.Fatalf("--no-sdl = %q, %v", got, err)
	}
	if _, err := resolvePackSDL(packOptions{OS: "linux", Arch: "riscv64"}, io.Discard); err == nil || !strings.Contains(err.Error(), "no SDL3 binary") {
		t.Fatalf("unsupported Linux arch error = %v", err)
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

func TestSDLBottlesArePinned(t *testing.T) {
	for arch, bottle := range sdlBottles {
		if len(bottle.digest) != 64 {
			t.Fatalf("%s digest = %q", arch, bottle.digest)
		}
	}
	if sdlBottles["amd64"].machine != 62 || sdlBottles["arm64"].machine != 183 {
		t.Fatalf("bottle machines = %+v", sdlBottles)
	}
}

func TestExtractBottledSDL(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "sdl3.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "sdl3/3.4.20/lib/libSDL3.so.0", Typeflag: tar.TypeSymlink, Linkname: "libSDL3.so.0.4.20"}); err != nil {
		t.Fatal(err)
	}
	elf := []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(elf[18:20], 183)
	payload := append(elf, []byte("payload")...)
	if err := tw.WriteHeader(&tar.Header{Name: "sdl3/3.4.20/lib/libSDL3.so.0.4.20", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractBottledSDL(archive, 183, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(got); err != nil || !bytes.Equal(b, payload) {
		t.Fatalf("extracted = %d bytes, %v", len(b), err)
	}
	if _, err := extractBottledSDL(archive, 62, dir); err == nil || !strings.Contains(err.Error(), "ELF machine") {
		t.Fatalf("wrong architecture error = %v", err)
	}
}

func TestCheckSDLLibrary(t *testing.T) {
	elf := func(machine uint16) []byte {
		b := make([]byte, 64)
		copy(b, "\x7fELF")
		binary.LittleEndian.PutUint16(b[18:20], machine)
		return b
	}
	pe := func(machine uint16) []byte {
		b := make([]byte, 0x80)
		copy(b, "MZ")
		binary.LittleEndian.PutUint32(b[0x3c:0x40], 0x40)
		copy(b[0x40:], "PE\x00\x00")
		binary.LittleEndian.PutUint16(b[0x44:0x46], machine)
		return b
	}
	thin := func(cpu uint32) []byte {
		b := make([]byte, 32)
		binary.BigEndian.PutUint32(b[0:4], 0xcffaedfe) // cf fa ed fe: little-endian 64-bit
		binary.LittleEndian.PutUint32(b[4:8], cpu)
		return b
	}
	fat := func(cpu uint32) []byte {
		b := make([]byte, 32)
		binary.BigEndian.PutUint32(b[0:4], 0xcafebabe)
		binary.BigEndian.PutUint32(b[4:8], 1)
		binary.BigEndian.PutUint32(b[8:12], cpu)
		return b
	}
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	ok := map[string]string{
		"linux/amd64":   write("elf-amd64", elf(62)),
		"linux/arm64":   write("elf-arm64", elf(183)),
		"windows/amd64": write("pe-amd64", pe(0x8664)),
		"windows/arm64": write("pe-arm64", pe(0xAA64)),
		"darwin/amd64":  write("thin-amd64", thin(0x01000007)),
		"darwin/arm64":  write("fat-universal", fat(0x0100000c)),
	}
	for target, path := range ok {
		parts := strings.Split(target, "/")
		if err := checkSDLLibrary(path, parts[0], parts[1]); err != nil {
			t.Fatalf("%s rejected: %v", target, err)
		}
	}
	bad := []struct{ target, path string }{
		{"linux/arm64", ok["linux/amd64"]},
		{"windows/arm64", ok["windows/amd64"]},
		{"darwin/arm64", ok["darwin/amd64"]},
		{"linux/amd64", write("not-a-lib", []byte("hello"))},
	}
	for _, c := range bad {
		parts := strings.Split(c.target, "/")
		if err := checkSDLLibrary(c.path, parts[0], parts[1]); err == nil {
			t.Fatalf("%s accepted a wrong library", c.target)
		}
	}
}

func TestVersionDigitsDropsEmptyParts(t *testing.T) {
	if got := versionDigits("1..2"); got != "1.2" {
		t.Fatalf("versionDigits(1..2) = %q, want 1.2", got)
	}
}

// fakeSDL writes the smallest object file checkSDLLibrary accepts.
func fakeSDL(t *testing.T, goos, goarch string) string {
	t.Helper()
	var b []byte
	switch goos {
	case "linux":
		b = make([]byte, 64)
		copy(b, "\x7fELF")
		binary.LittleEndian.PutUint16(b[18:20], elfMachines[goarch])
	case "darwin":
		b = make([]byte, 32)
		binary.BigEndian.PutUint32(b[0:4], 0xcffaedfe)
		binary.LittleEndian.PutUint32(b[4:8], machoCPUs[goarch])
	case "windows":
		b = make([]byte, 0x80)
		copy(b, "MZ")
		binary.LittleEndian.PutUint32(b[0x3c:0x40], 0x40)
		copy(b[0x40:], "PE\x00\x00")
		binary.LittleEndian.PutUint16(b[0x44:0x46], peMachines[goarch])
	}
	path := filepath.Join(t.TempDir(), packSDLFile(goos))
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
