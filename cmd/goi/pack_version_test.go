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

func TestVersionDigits(t *testing.T) {
	cases := map[string]string{
		"1.2.3":        "1.2.3",
		"v0.0.1":       "0.0.1",
		"V2":           "2",
		"1.2.3-beta.1": "1.2.3",
		"nightly":      "",
		"2026.11+abc":  "2026.11",
	}
	for in, want := range cases {
		if got := versionDigits(in); got != want {
			t.Fatalf("versionDigits(%q) = %q, want %q", in, got, want)
		}
	}
	cases = map[string]string{
		"1.2.3":       "1.2.3.0",
		"v0.0.1":      "0.0.1.0",
		"1.2.3-beta":  "1.2.3.0",
		"nightly":     "0.0.0.0",
		"1":           "1.0.0.0",
		"1.2.3.4.5.6": "1.2.3.4",
	}
	for in, want := range cases {
		if got := nsisFileVersion(in); got != want {
			t.Fatalf("nsisFileVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPackDarwinVersion(t *testing.T) {
	dir, goRun := packVersionProject(t)
	sdl := fakeSDL(t, "darwin", "amd64")
	for version, want := range map[string]string{
		"1.2.3-beta": "1.2.3",
		"v0.0.1":     "0.0.1",
	} {
		out, err := packProject(dir, packOptions{OS: "darwin", Arch: "amd64", Version: version, SDL: sdl},
			goRun, func(string, []string, []string, io.Writer, io.Writer) error { return nil }, io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		plist, err := os.ReadFile(filepath.Join(out, "Contents", "Info.plist"))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"CFBundleShortVersionString", "CFBundleVersion"} {
			field := "<key>" + key + "</key>\n\t<string>" + want + "</string>"
			if !strings.Contains(string(plist), field) {
				t.Fatalf("%s: %s lacks %q:\n%s", version, key, want, plist)
			}
		}
	}
}

func TestPackWindowsVersion(t *testing.T) {
	dir, goRun := packVersionProject(t)
	for version, fixed := range map[string]string{
		"1.2.3":      "1.2.3.0",
		"v0.0.1":     "0.0.1.0",
		"1.2.3-beta": "1.2.3.0",
	} {
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
			OS: "windows", Arch: "amd64", Version: version, Makensis: "/fake/makensis", NoSDL: true, Keep: true,
		}, goRun, runTool, io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		script, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatal(err)
		}
		os.RemoveAll(filepath.Dir(scriptPath))
		if !strings.Contains(out, version) {
			t.Fatalf("%s: out = %q, want the version in the name", version, out)
		}
		for _, want := range []string{
			`"DisplayVersion" "` + version + `"`,
			`VIAddVersionKey "ProductVersion" "` + version + `"`,
			`VIProductVersion "` + fixed + `"`,
		} {
			if !strings.Contains(string(script), want) {
				t.Fatalf("%s: installer script lacks %q:\n%s", version, want, script)
			}
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
