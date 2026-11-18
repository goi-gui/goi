//go:build !js && !wasip1

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
	opts := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "example.com/myapp", UI: "ui"}
	if err := initProject(dir, opts, func(d string, args []string) error {
		calls = append(calls, append([]string{d}, args...))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0][1] != "mod" || calls[0][2] != "init" || calls[0][3] != "example.com/myapp" {
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
	if cfg.Name != "myapp" {
		t.Fatalf("name = %q, want myapp", cfg.Name)
	}
	if len(cfg.BuildFlags) != 1 || cfg.BuildFlags[0] != "all" {
		t.Fatalf("buildFlags = %v, want [all]", cfg.BuildFlags)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "goi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: myapp", "id: com.example.myapp", "buildFlags: [all]", "ui: ui"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("goi.yaml lacks %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "entry:") {
		t.Fatalf("goi.yaml still names an entry:\n%s", raw)
	}
}

func TestInitProjectUsesTheUIDirectory(t *testing.T) {
	dir := t.TempDir()
	opts := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "example.com/myapp", UI: "designs"}
	if err := initProject(dir, opts, func(string, []string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "designs", "Main.goi")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "goi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ui: designs") {
		t.Fatalf("goi.yaml lacks the ui flag:\n%s", raw)
	}
}

func TestInitProjectNeedsEveryValue(t *testing.T) {
	err := initProject(t.TempDir(), initOptions{Name: "myapp", Module: "example.com/myapp"}, func(string, []string) error { return nil })
	if err == nil {
		t.Fatal("init accepted a missing id")
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
	opts := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "example.com/myapp", UI: "ui"}
	if err := initProject(dir, opts, func(string, []string) error { return nil }); err == nil {
		t.Fatal("init overwrote an existing cmd/main.go")
	}
}

func TestParseInitFlags(t *testing.T) {
	opts, err := parseInit([]string{
		"--name", "myapp",
		"--id", "com.example.myapp",
		"--module", "github.com/me/myapp",
		"--ui", "designs",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "github.com/me/myapp", UI: "designs"}
	if opts != want {
		t.Fatalf("opts = %+v, want %+v", opts, want)
	}

	// A flag that is missing stays empty for the prompt; ui has a default.
	opts, err = parseInit([]string{"--name", "myapp"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Name != "myapp" || opts.ID != "" || opts.Module != "" || opts.UI != "ui" {
		t.Fatalf("opts = %+v, want only name and the ui default", opts)
	}
}

func TestBuildProject(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	dir := buildProjectDir(t, "name: myapp\nbuildFlags: [emoji]\n")

	var gotDir string
	var gotArgs, gotEnv []string
	out, err := buildProject(dir, func(d string, args []string, _, _ io.Writer, env []string) error {
		gotDir, gotArgs, gotEnv = d, args, env
		return nil
	}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	wantOut := filepath.Join(dir, "dist", "linux-amd64", "myapp")
	if out != wantOut {
		t.Fatalf("out = %q, want %q", out, wantOut)
	}
	if gotDir != dir {
		t.Fatalf("dir = %q, want %q", gotDir, dir)
	}
	wantArgs := []string{"build", "-tags", "nofakecgo,emoji,nohotreload", "-o", wantOut, "./cmd"}
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
	if _, err := os.Stat(filepath.Dir(wantOut)); err != nil {
		t.Fatalf("output directory was not created: %v", err)
	}
}

func TestBuildProjectAddsExeForWindows(t *testing.T) {
	t.Setenv("GOOS", "windows")
	t.Setenv("GOARCH", "arm64")
	dir := buildProjectDir(t, "name: myapp\nbuildFlags: [all]\n")

	out, err := buildProject(dir, func(string, []string, io.Writer, io.Writer, []string) error { return nil }, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "dist", "windows-arm64", "myapp.exe")
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestBuildProjectNeedsAName(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	dir := buildProjectDir(t, "buildFlags: [all]\n")

	_, err := buildProject(dir, func(string, []string, io.Writer, io.Writer, []string) error { return nil }, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("err = %v, want a name hint", err)
	}
}

// buildProjectDir makes the smallest project goi build accepts.
func buildProjectDir(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
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
