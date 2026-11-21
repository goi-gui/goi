//go:build !js && !wasip1

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitProjectWritesTheScaffold(t *testing.T) {
	dir := t.TempDir()
	var calls [][]string
	opts := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "example.com/myapp", Assets: "assets"}
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
	for _, want := range []string{`LoadWindow("ui/Main.goi")`} {
		if !strings.Contains(string(main), want) {
			t.Errorf("main.go lacks %q:\n%s", want, main)
		}
	}
	design, err := os.ReadFile(filepath.Join(dir, "assets", "ui", "Main.goi"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Hello, World!"`, `goi://images/goi.svg`, "mainAxisAlign:  center", "crossAxisAlign: center"} {
		if !strings.Contains(string(design), want) {
			t.Errorf("design lacks %q:\n%s", want, design)
		}
	}
	logo, err := os.ReadFile(filepath.Join(dir, "assets", "images", "goi.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logo), "<svg") || !strings.Contains(string(logo), "GOI logo") {
		t.Fatalf("logo is not the hello example mark:\n%s", logo)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "myapp" {
		t.Fatalf("name = %q, want myapp", cfg.Name)
	}
	if len(cfg.BuildFlags) != 1 || cfg.BuildFlags[0] != "auto" {
		t.Fatalf("buildFlags = %v, want [auto]", cfg.BuildFlags)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "goi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: myapp", "id: com.example.myapp", "runFlags: [all]", "buildFlags: [auto]", "assets: assets", "entrypoint: cmd"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("goi.yaml lacks %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "entry:") {
		t.Fatalf("goi.yaml still names an entry:\n%s", raw)
	}
}

func TestInitProjectUsesTheAssetsDirectory(t *testing.T) {
	dir := t.TempDir()
	opts := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "example.com/myapp", Assets: "designs"}
	if err := initProject(dir, opts, func(string, []string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "designs", "ui", "Main.goi")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "designs", "images", "goi.svg")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "goi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "assets: designs") {
		t.Fatalf("goi.yaml lacks the assets flag:\n%s", raw)
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
	opts := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "example.com/myapp", Assets: "assets"}
	if err := initProject(dir, opts, func(string, []string) error { return nil }); err == nil {
		t.Fatal("init overwrote an existing cmd/main.go")
	}
}

func TestInitCommandFlags(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var calls [][]string
	root := newRootCmd(func(d string, args []string) error {
		calls = append(calls, append([]string{d}, args...))
		return nil
	}, func(string, []string, io.Writer, io.Writer, []string) error { return nil })
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{
		"init",
		"--name", "myapp",
		"--id", "com.example.myapp",
		"--module", "github.com/me/myapp",
		"--assets", "designs",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0][1] != "mod" || calls[0][2] != "init" || calls[0][3] != "github.com/me/myapp" {
		t.Fatalf("go calls = %v", calls)
	}
	if calls[1][1] != "mod" || calls[1][2] != "tidy" {
		t.Fatalf("second go call = %v, want mod tidy", calls[1])
	}
	if _, err := os.Stat(filepath.Join(dir, "designs", "ui", "Main.goi")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "designs", "images", "goi.svg")); err != nil {
		t.Fatal(err)
	}
}

func TestBuildProject(t *testing.T) {
	// The caller env is only a default: the target flags win.
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	dir := buildProjectDir(t, "name: myapp\nrunFlags: [code]\nbuildFlags: [emoji]\n")

	var gotDir string
	var gotArgs, gotEnv []string
	out, err := buildProject(dir, "windows", "arm64", func(d string, args []string, _, _ io.Writer, env []string) error {
		gotDir, gotArgs, gotEnv = d, args, env
		assets, err := os.ReadFile(filepath.Join(d, "assets", "goi_embed_gen.go"))
		if err != nil {
			t.Errorf("assets embed file missing: %v", err)
		} else if !strings.Contains(string(assets), "//go:embed all:*") {
			t.Errorf("assets embed file has no go:embed:\n%s", assets)
		}
		embed, err := os.ReadFile(filepath.Join(d, "cmd", "goi_embed_gen.go"))
		if err != nil {
			t.Errorf("command embed file missing: %v", err)
		} else if !strings.Contains(string(embed), "ui.RegisterAssets") {
			t.Errorf("command embed file does not register the design:\n%s", embed)
		}
		return nil
	}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	wantOut := filepath.Join(dir, "dist", "windows-arm64", "myapp.exe")
	if out != wantOut {
		t.Fatalf("out = %q, want %q", out, wantOut)
	}
	if gotDir != dir {
		t.Fatalf("dir = %q, want %q", gotDir, dir)
	}
	wantArgs := []string{"build", "-tags", "nofakecgo,emoji",
		"-trimpath", "-ldflags", "-s -w -H windowsgui", "-o", wantOut, "./cmd"}
	if strings.Join(gotArgs, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
	for _, file := range []string{"assets/goi_embed_gen.go", "cmd/goi_embed_gen.go"} {
		if _, err := os.Stat(filepath.Join(dir, file)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s was not cleaned up", file)
		}
	}
	if got := envValue(gotEnv, "CGO_ENABLED"); len(envValues(gotEnv, "CGO_ENABLED")) != 1 || got != "0" {
		t.Fatalf("CGO_ENABLED = %q in %v, want exactly 0", got, gotEnv)
	}
	if got := envValue(gotEnv, "GOOS"); got != "windows" {
		t.Fatalf("GOOS = %q, want windows", got)
	}
	if got := envValue(gotEnv, "GOARCH"); got != "arm64" {
		t.Fatalf("GOARCH = %q, want arm64", got)
	}
	if _, err := os.Stat(filepath.Dir(wantOut)); err != nil {
		t.Fatalf("output directory was not created: %v", err)
	}
}

func TestBuildCommandTargetFlags(t *testing.T) {
	dir := buildProjectDir(t, "name: myapp\n")
	t.Chdir(dir)
	var gotEnv []string
	cmd := newBuildCmd(func(_ string, _ []string, _, _ io.Writer, env []string) error {
		gotEnv = env
		return nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--os", "windows", "--arch", "arm64"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := envValue(gotEnv, "GOOS"); got != "windows" {
		t.Fatalf("GOOS = %q, want windows", got)
	}
	if got := envValue(gotEnv, "GOARCH"); got != "arm64" {
		t.Fatalf("GOARCH = %q, want arm64", got)
	}
}

func TestBuildLDFlags(t *testing.T) {
	cases := map[string]string{
		"linux":   "-s -w",
		"darwin":  "-s -w",
		"windows": "-s -w -H windowsgui",
	}
	for target, want := range cases {
		if got := buildLDFlags(target); got != want {
			t.Errorf("buildLDFlags(%q) = %q, want %q", target, got, want)
		}
	}
}

func TestBuildOutput(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		os, arch, name, want string
	}{
		{"linux", "amd64", "myapp", "myapp"},
		{"windows", "amd64", "myapp", "myapp.exe"},
		{"windows", "arm64", "myapp.exe", "myapp.exe"},
		{"js", "wasm", "myapp", "myapp.wasm"},
	}
	for _, c := range cases {
		got := buildOutput(dir, c.os, c.arch, c.name)
		want := filepath.Join(dir, "dist", c.os+"-"+c.arch, c.want)
		if got != want {
			t.Errorf("buildOutput(%s/%s, %q) = %q, want %q", c.os, c.arch, c.name, got, want)
		}
	}
}

func TestBuildProjectNeedsAName(t *testing.T) {
	dir := buildProjectDir(t, "buildFlags: [all]\n")

	_, err := buildProject(dir, runtime.GOOS, runtime.GOARCH, func(string, []string, io.Writer, io.Writer, []string) error { return nil }, io.Discard, io.Discard)
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
	if err := os.MkdirAll(filepath.Join(dir, "assets", "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "ui", "Main.goi"), []byte("Window { }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "cover.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunProjectUsesRunFlagsAndEnvironment(t *testing.T) {
	t.Setenv("GOI_TEST_ENV", "kept")
	t.Setenv("GOI_DEBUG", "FALSE")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte("runFlags: [emoji, \"svg net\"]\nbuildFlags: [code]\n"), 0o644); err != nil {
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
	if got := envValue(gotEnv, "GOI_TEST_ENV"); got != "kept" {
		t.Fatalf("GOI_TEST_ENV = %q, want the caller's value", got)
	}
	if got := envValue(gotEnv, "GOI_DEBUG"); got != "FALSE" {
		t.Fatalf("GOI_DEBUG = %q, want the caller's FALSE", got)
	}
	if got := envValue(gotEnv, "LOGGER_OUTPUT_FORMAT"); got != "logfmt" {
		t.Fatalf("LOGGER_OUTPUT_FORMAT = %q, want logfmt", got)
	}
}

func TestRunEnvDefaultsDebugOn(t *testing.T) {
	t.Setenv("GOI_DEBUG", "FALSE")
	if err := os.Unsetenv("GOI_DEBUG"); err != nil {
		t.Fatal(err)
	}
	env := runEnv()
	if got := envValue(env, "GOI_DEBUG"); got != "TRUE" {
		t.Fatalf("GOI_DEBUG = %q, want TRUE", got)
	}
	if got := envValue(env, "CGO_ENABLED"); got != "0" {
		t.Fatalf("CGO_ENABLED = %q, want 0", got)
	}
	if got := envValue(env, "LOGGER_OUTPUT_FORMAT"); got != "logfmt" {
		t.Fatalf("LOGGER_OUTPUT_FORMAT = %q, want logfmt", got)
	}
}

// envValue is the last value of key in env, or "".
func envValue(env []string, key string) string {
	values := envValues(env, key)
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func envValues(env []string, key string) []string {
	var out []string
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && strings.EqualFold(k, key) {
			out = append(out, v)
		}
	}
	return out
}

func TestLoadConfigDefaultsToAll(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte("assets: assets\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.RunFlags) != 1 || cfg.RunFlags[0] != "all" {
		t.Fatalf("runFlags = %v, want [all]", cfg.RunFlags)
	}
	if len(cfg.BuildFlags) != 1 || cfg.BuildFlags[0] != "all" {
		t.Fatalf("buildFlags = %v, want [all]", cfg.BuildFlags)
	}
	if cfg.Entrypoint != "cmd" {
		t.Fatalf("entrypoint = %q, want cmd", cfg.Entrypoint)
	}
	if cfg.Assets != "assets" {
		t.Fatalf("assets = %q, want assets", cfg.Assets)
	}
}

func TestCommandsUseEntrypoint(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "only-app.txt"), []byte("app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets", "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "ui", "Main.goi"), []byte("Window { }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := "name: myapp\nentrypoint: app\nbuildFlags: [all]\n"
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	var runArgs, buildArgs []string
	run := func(_ string, args []string, _, _ io.Writer, _ []string) error {
		runArgs = args
		return nil
	}
	if err := runProject(dir, run, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if last := runArgs[len(runArgs)-1]; last != "./app" {
		t.Fatalf("run package = %q, want ./app", last)
	}

	stagedApp := false
	build := func(d string, args []string, _, _ io.Writer, _ []string) error {
		buildArgs = args
		_, err := os.Stat(filepath.Join(d, "app", "only-app.txt"))
		stagedApp = err == nil
		if _, err := os.Stat(filepath.Join(d, "app", "goi_embed_gen.go")); err != nil {
			t.Errorf("embed file missing from app: %v", err)
		}
		return nil
	}
	if _, err := buildProject(dir, runtime.GOOS, runtime.GOARCH, build, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !stagedApp {
		t.Fatalf("build did not stage the app entrypoint: %v", buildArgs)
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

func TestInitProjectWritesGitignore(t *testing.T) {
	dir := t.TempDir()
	opts := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "example.com/myapp", Assets: "assets"}
	if err := initProject(dir, opts, func(string, []string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "dist/") {
		t.Fatalf(".gitignore = %q, want dist/", b)
	}

	// An existing file keeps its entries.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(b), "node_modules/") || !strings.Contains(string(b), "dist/") {
		t.Fatalf(".gitignore = %q", b)
	}
}

func TestInitProjectOverwritesWithTheFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module old.example/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "example.com/myapp", Assets: "assets", Overwrite: true}
	var calls int
	if err := initProject(dir, opts, func(string, []string) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("go mod init ran over an existing go.mod: %d calls", calls)
	}
	main, err := os.ReadFile(filepath.Join(dir, "cmd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(main) == "keep" || !strings.Contains(string(main), "LoadWindow") {
		t.Fatalf("main.go was not overwritten:\n%s", main)
	}
}

func TestInitProjectInitializesGit(t *testing.T) {
	old := gitInit
	t.Cleanup(func() { gitInit = old })
	var called string
	gitInit = func(dir string) error { called = dir; return nil }
	dir := t.TempDir()
	opts := initOptions{Name: "myapp", ID: "com.example.myapp", Module: "example.com/myapp", Assets: "assets", Git: true}
	if err := initProject(dir, opts, func(string, []string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if called != dir {
		t.Fatalf("git init dir = %q, want %q", called, dir)
	}
}
