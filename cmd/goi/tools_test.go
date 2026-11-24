//go:build !js && !wasip1

package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ulikunitz/xz"
)

// writeTarXZ writes one tar.xz holding name -> body entries.
func writeTarXZ(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	xw, err := xz.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(xw)
	for name, body := range files {
		mode := int64(0o644)
		if filepath.Base(name) == "makensis" {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeAr writes a Unix ar archive, the container a .deb uses.
func writeAr(t *testing.T, path string, members map[string]string) {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("!<arch>\n")
	for name, body := range members {
		hdr := fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10s`\n", name, "0", "0", "0", "100644", fmt.Sprint(len(body)))
		b.WriteString(hdr)
		b.WriteString(body)
		if len(body)%2 == 1 {
			b.WriteByte('\n')
		}
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractDebData(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.tar.xz")
	writeTarXZ(t, data, map[string]string{
		"./usr/bin/makensis":                      "bin",
		"./usr/share/nsis/Stubs/zlib-x86-unicode": "stub",
		"./usr/share/doc/nsis/README":             "skip",
	})
	deb := filepath.Join(dir, "nsis.deb")
	writeAr(t, deb, map[string]string{
		"debian-binary": "2.0\n",
		"data.tar.xz":   string(mustRead(t, data)),
	})
	root := filepath.Join(dir, "root")
	if err := extractDebData(deb, root); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(root, "usr", "bin", "makensis"))); got != "bin" {
		t.Fatalf("makensis = %q", got)
	}
	if got := string(mustRead(t, filepath.Join(root, "usr", "share", "nsis", "Stubs", "zlib-x86-unicode"))); got != "stub" {
		t.Fatalf("stub = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "usr", "share", "doc", "nsis", "README")); !os.IsNotExist(err) {
		t.Fatalf("a file outside usr/share/nsis was unpacked: %v", err)
	}
	if info, err := os.Stat(filepath.Join(root, "usr", "bin", "makensis")); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("makensis is not executable: %v", err)
	}
}

func TestReadArMemberMissing(t *testing.T) {
	dir := t.TempDir()
	deb := filepath.Join(dir, "x.deb")
	writeAr(t, deb, map[string]string{"debian-binary": "2.0\n"})
	if err := readArMember(deb, "data.tar.xz", func(io.Reader) error { return nil }); err == nil {
		t.Fatal("a missing ar member was reported as found")
	}
}

func TestExtractZipDir(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "nsis.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"nsis-3.11/Bin/makensis.exe": "exe",
		"nsis-3.11/Stubs/uninst":     "stub",
		"other/ignored":              "no",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "root")
	if err := extractZipDir(archive, "nsis-3.11/", root); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(root, "Bin", "makensis.exe"))); got != "exe" {
		t.Fatalf("makensis = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "other", "ignored")); !os.IsNotExist(err) {
		t.Fatalf("a file outside the prefix was unpacked: %v", err)
	}
}
