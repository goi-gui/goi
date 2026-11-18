package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDesignDirReadsUI: goi.yaml names the directory the entry is resolved in.
func TestDesignDirReadsUI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte("buildFlags: [all]\nui: designs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	got, err := designDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "designs" {
		t.Fatalf("designDir = %q, want designs", got)
	}
}

// TestDesignDirWithoutConfig: a single-file program needs no goi.yaml, so its
// entry is used as written.
func TestDesignDirWithoutConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	got, err := designDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "." {
		t.Fatalf("designDir = %q, want the working directory", got)
	}
}

// TestDesignDirDefaultsUI: a config without ui still has a design directory.
func TestDesignDirDefaultsUI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte("buildFlags: [all]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	got, err := designDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "ui" {
		t.Fatalf("designDir = %q, want ui", got)
	}
}

func TestDesignDirReportsBadYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte("ui: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if _, err := designDir(); err == nil {
		t.Fatal("expected a goi.yaml parse error")
	}
}
