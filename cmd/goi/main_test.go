package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitProjectWritesTheScaffold(t *testing.T) {
	dir := t.TempDir()
	var calls [][]string
	if err := initProject(dir, "example.com/hello", func(d string, args []string) error {
		calls = append(calls, append([]string{d}, args...))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0][1] != "mod" || calls[0][2] != "init" || calls[0][3] != "example.com/hello" {
		t.Fatalf("go calls = %v", calls)
	}
	main, err := os.ReadFile(filepath.Join(dir, "cmd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`LoadWindow("Main.goi")`} {
		if !strings.Contains(string(main), want) {
			t.Errorf("main.go lacks %q:\n%s", want, main)
		}
	}
	design, err := os.ReadFile(filepath.Join(dir, "ui", "Main.goi"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(design), `"Hello, world!"`) || !strings.Contains(string(design), "mainAxisAlign:  center") {
		t.Fatalf("design does not centre hello world:\n%s", design)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.BuildFlags) != 1 || cfg.BuildFlags[0] != "all" {
		t.Fatalf("buildFlags = %v, want [all]", cfg.BuildFlags)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "goi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "buildFlags: [all]") || !strings.Contains(string(raw), "ui: ui") {
		t.Fatalf("goi.yaml lacks the design settings:\n%s", raw)
	}
	if strings.Contains(string(raw), "entry:") {
		t.Fatalf("goi.yaml still names an entry:\n%s", raw)
	}
}

func TestInitProjectRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := initProject(dir, "example.com/hello", func(string, []string) error { return nil }); err == nil {
		t.Fatal("init overwrote an existing cmd/main.go")
	}
}

func TestRunProjectUsesBuildFlagsAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte("buildFlags: [emoji, \"svg net\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotDir string
	var gotArgs, gotEnv []string
	err := runProject(dir, func(d string, args []string, _, _ io.Writer, env []string) error {
		gotDir, gotArgs, gotEnv = d, args, env
		return nil
	}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != dir {
		t.Fatalf("dir = %q, want %q", gotDir, dir)
	}
	wantArgs := []string{"run", "-tags", "nofakecgo,emoji,svg,net", "./cmd"}
	if strings.Join(gotArgs, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
	var cgo []string
	for _, e := range gotEnv {
		if strings.HasPrefix(e, "CGO_ENABLED=") {
			cgo = append(cgo, e)
		}
	}
	if len(cgo) != 1 || cgo[0] != "CGO_ENABLED=0" {
		t.Fatalf("CGO_ENABLED entries = %v, want exactly 0", cgo)
	}
}

func TestLoadConfigDefaultsToAll(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte("ui: ui\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.BuildFlags) != 1 || cfg.BuildFlags[0] != "all" {
		t.Fatalf("buildFlags = %v, want [all]", cfg.BuildFlags)
	}
}

func TestBuildTagsDedupesAndSplits(t *testing.T) {
	got := buildTags([]string{"emoji,svg", "emoji", " all ", "nofakecgo"})
	if strings.Join(got, ",") != "nofakecgo,emoji,svg,all" {
		t.Fatalf("tags = %v", got)
	}
}

func TestRunProjectNeedsGoiYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runProject(dir, func(string, []string, io.Writer, io.Writer, []string) error { return nil }, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "goi.yaml") {
		t.Fatalf("err = %v, want a goi.yaml hint", err)
	}
}
