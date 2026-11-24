//go:build !js && !wasip1

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidPackageVersion(t *testing.T) {
	for _, v := range []string{"", "1", "1.2.3", "1.2.3-beta.1", "2026.11+local"} {
		if !validPackageVersion(v) {
			t.Fatalf("%q was rejected", v)
		}
	}
	for _, v := range []string{"1 2", "1/2", "v\n1", ".", "-beta"} {
		if validPackageVersion(v) {
			t.Fatalf("%q was accepted", v)
		}
	}
}

// packVersionProject is the smallest project that packs with a version.
func packVersionProject(t *testing.T) (string, runFunc) {
	t.Helper()
	dir := buildProjectDir(t, "name: myapp\nid: com.example.myapp\n")
	writePackIcon(t, filepath.Join(dir, "assets", "icon.png"), 256, 256)
	return dir, func(_ string, args []string, _, _ io.Writer, _ []string) error {
		out := args[len(args)-2]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, []byte("binary"), 0o755)
	}
}

func TestPackLinuxVersion(t *testing.T) {
	dir, goRun := packVersionProject(t)
	var appDir string
	var gotEnv []string
	runTool := func(_ string, args, env []string, _, _ io.Writer) error {
		appDir = args[1]
		gotEnv = env
		return os.WriteFile(args[2], []byte("image"), 0o755)
	}
	out, err := packProject(dir, packOptions{
		OS: "linux", Arch: "amd64", Version: "1.2.3", Tool: "/fake", NoSDL: true, Keep: true,
	}, goRun, runTool, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(appDir)
	want := filepath.Join(dir, "dist", "linux-amd64", "myapp-1.2.3-x86_64.AppImage")
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	desktop, err := os.ReadFile(filepath.Join(appDir, "com.example.myapp.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(desktop), "X-AppImage-Version=1.2.3") {
		t.Fatalf("desktop entry lacks the version:\n%s", desktop)
	}
	if got := envValue(gotEnv, "VERSION"); got != "1.2.3" {
		t.Fatalf("VERSION = %q, want 1.2.3", got)
	}
}

func TestPackDarwinVersion(t *testing.T) {
	dir, goRun := packVersionProject(t)
	sdl := filepath.Join(t.TempDir(), "libSDL3.0.dylib")
	if err := os.WriteFile(sdl, []byte("dylib"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := packProject(dir, packOptions{OS: "darwin", Arch: "amd64", Version: "1.2.3-beta", SDL: sdl},
		goRun, func(string, []string, []string, io.Writer, io.Writer) error { return nil }, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	plist, err := os.ReadFile(filepath.Join(out, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<string>1.2.3-beta</string>", "<key>CFBundleVersion</key>\n\t<string>1.2.3</string>"} {
		if !strings.Contains(string(plist), want) {
			t.Fatalf("Info.plist lacks %q:\n%s", want, plist)
		}
	}
}

func TestPackWindowsVersion(t *testing.T) {
	dir, goRun := packVersionProject(t)
	var scriptPath string
	runTool := func(_ string, args []string, _ []string, _, _ io.Writer) error {
		scriptPath = args[0]
		b, err := os.ReadFile(scriptPath)
		if err != nil {
			return err
		}
		out, ok := nsisOutFile(string(b))
		if !ok {
			t.Fatalf("no OutFile:\n%s", b)
		}
		return os.WriteFile(filepath.FromSlash(out), []byte("setup"), 0o755)
	}
	out, err := packProject(dir, packOptions{
		OS: "windows", Arch: "amd64", Version: "1.2.3", Makensis: "/fake/makensis", NoSDL: true, Keep: true,
	}, goRun, runTool, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(scriptPath))
	want := filepath.Join(dir, "dist", "windows-amd64", "myapp-1.2.3-amd64-setup.exe")
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"DisplayVersion" "1.2.3"`,
		`VIAddVersionKey "ProductVersion" "1.2.3"`,
		`VIProductVersion "1.2.3.0"`,
	} {
		if !strings.Contains(string(script), want) {
			t.Fatalf("installer script lacks %q:\n%s", want, script)
		}
	}
}

func TestPackRejectsABadVersion(t *testing.T) {
	dir, goRun := packVersionProject(t)
	_, err := packProject(dir, packOptions{OS: "linux", Arch: "amd64", Version: "1 2", NoSDL: true},
		goRun, func(string, []string, []string, io.Writer, io.Writer) error { return nil }, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("err = %v, want a version hint", err)
	}
}
