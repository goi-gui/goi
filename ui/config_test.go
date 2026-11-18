package ui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestReadWindowConfigMergesGoiAndUI: goi.yaml supplies the directories and
// entry; a ui.yaml overrides only the fields it writes.
func TestReadWindowConfigMergesGoiAndUI(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("goi.yaml", "extensions: [emoji]\nui: [ui, shared]\nentry: Main.goi\n")
	write("ui.yaml", "entry: Other.goi\n")

	cfg, err := readWindowConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.UI) != 2 || cfg.UI[0] != "ui" || cfg.UI[1] != "shared" {
		t.Fatalf("ui = %v", cfg.UI)
	}
	if cfg.Entry != "Other.goi" {
		t.Fatalf("entry = %q, want the ui.yaml override", cfg.Entry)
	}
}

func TestReadWindowConfigDefaults(t *testing.T) {
	cfg, err := readWindowConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.UI) != 0 || cfg.Entry != "" {
		t.Fatalf("defaults = %+v, want the loader's ui/Main.goi fallback", cfg)
	}
}

// TestDirsFSFindsFilesInEveryRoot: a design split over two directories is one
// tree to the loader.
func TestDirsFSFindsFilesInEveryRoot(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(a, "Main.goi"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "Part.goi"), []byte("part"), 0o644); err != nil {
		t.Fatal(err)
	}
	fsys := dirsFS{a, b}
	if got, err := fs.ReadFile(fsys, "Main.goi"); err != nil || string(got) != "main" {
		t.Fatalf("Main.goi = %q, %v", got, err)
	}
	if got, err := fs.ReadFile(fsys, "Part.goi"); err != nil || string(got) != "part" {
		t.Fatalf("Part.goi = %q, %v", got, err)
	}
	if _, err := fs.ReadFile(fsys, "Missing.goi"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file error = %v", err)
	}
}
