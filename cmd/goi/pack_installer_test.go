//go:build !js && !wasip1

package main

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPackWindows(t *testing.T) {
	dir := buildProjectDir(t, "name: myapp\nid: com.example.myapp\n")
	writePackIcon(t, filepath.Join(dir, "assets", "icon.png"), 256, 256)

	goRun := func(_ string, args []string, _, _ io.Writer, _ []string) error {
		out := args[len(args)-2]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, []byte("PE"), 0o755)
	}
	var scriptPath string
	runTool := func(path string, args []string, _ []string, _, _ io.Writer) error {
		if path != "/fake/makensis" {
			t.Fatalf("tool = %q", path)
		}
		scriptPath = args[0]
		b, err := os.ReadFile(scriptPath)
		if err != nil {
			return err
		}
		out, ok := nsisOutFile(string(b))
		if !ok {
			t.Fatalf("no OutFile in the installer script:\n%s", b)
		}
		return os.WriteFile(filepath.FromSlash(out), []byte("setup"), 0o755)
	}
	out, err := packProject(dir, packOptions{
		OS: "windows", Arch: "amd64", Makensis: "/fake/makensis", NoSDL: true, Keep: true,
	}, goRun, runTool, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(scriptPath))

	want := filepath.Join(dir, "dist", "windows-amd64", "myapp-amd64-setup.exe")
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	stage := filepath.Dir(scriptPath)
	if b, err := os.ReadFile(filepath.Join(stage, "myapp.exe")); err != nil || string(b) != "PE" {
		t.Fatalf("staged exe = %q, %v", b, err)
	}
	ico, err := os.ReadFile(filepath.Join(stage, "com.example.myapp.ico"))
	if err != nil {
		t.Fatal(err)
	}
	checkICO(t, ico)

	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`InstallDir "$LOCALAPPDATA\Programs\myapp"`,
		`RequestExecutionLevel user`,
		`WriteUninstaller "$INSTDIR\uninstall.exe"`,
		`CreateShortcut "$DESKTOP\myapp.lnk"`,
		`"DisplayName" "myapp"`,
		`Software\Microsoft\Windows\CurrentVersion\Uninstall\com.example.myapp`,
	} {
		if !strings.Contains(string(script), want) {
			t.Fatalf("installer script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(string(script), "SDL3.dll") {
		t.Fatalf("--no-sdl still staged SDL3:\n%s", script)
	}
}

// nsisOutFile reads the OutFile a generated script writes.
func nsisOutFile(script string) (string, bool) {
	for _, line := range strings.Split(script, "\n") {
		if after, ok := strings.CutPrefix(line, "OutFile \""); ok {
			return strings.TrimSuffix(after, "\""), true
		}
	}
	return "", false
}

func TestNSISScriptBundlesSDL(t *testing.T) {
	script := nsisScript(config{Name: "myapp", ID: "com.example.myapp"}, "/stage", "/out/myapp-setup.exe", "com.example.myapp.ico", true)
	for _, want := range []string{
		`File "/stage/SDL3.dll"`,
		`File "/stage/SDL3-LICENSE.txt"`,
		`Delete "$INSTDIR\SDL3.dll"`,
		`Delete "$INSTDIR\SDL3-LICENSE.txt"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("installer script lacks %q:\n%s", want, script)
		}
	}
}

func TestWriteICO(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "icon.png")
	writePackIcon(t, src, 256, 256)
	dst := filepath.Join(dir, "app.ico")
	if err := writeICO(src, dst); err != nil {
		t.Fatal(err)
	}
	checkICO(t, mustRead(t, dst))
}

func checkICO(t *testing.T, b []byte) {
	t.Helper()
	if len(b) < 6 {
		t.Fatalf("ico is %d bytes", len(b))
	}
	if got := binary.LittleEndian.Uint16(b[0:2]); got != 0 {
		t.Fatalf("ico reserved = %d", got)
	}
	if got := binary.LittleEndian.Uint16(b[2:4]); got != 1 {
		t.Fatalf("ico type = %d, want icon", got)
	}
	count := int(binary.LittleEndian.Uint16(b[4:6]))
	if count == 0 {
		t.Fatal("ico has no images")
	}
	off := 6 + 16*count
	seen256 := false
	for i := 0; i < count; i++ {
		entry := b[6+16*i : 6+16*(i+1)]
		size := int(binary.LittleEndian.Uint32(entry[8:12]))
		offset := int(binary.LittleEndian.Uint32(entry[12:16]))
		if offset != off || off+size > len(b) {
			t.Fatalf("ico image %d: offset %d size %d, file %d bytes", i, offset, size, len(b))
		}
		off += size
		if entry[0] == 0 && entry[1] == 0 {
			seen256 = true
		}
	}
	if off != len(b) {
		t.Fatalf("ico ends at %d, file has %d bytes", off, len(b))
	}
	if !seen256 {
		t.Fatal("ico has no 256x256 image")
	}
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPackLinuxNeedsLinuxHost(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("the guard only fires on another host")
	}
	dir := buildProjectDir(t, "name: myapp\nid: com.example.myapp\n")
	writePackIcon(t, filepath.Join(dir, "assets", "icon.png"), 256, 256)
	t.Chdir(dir)
	cmd := newPackCmd(func(string, []string, io.Writer, io.Writer, []string) error { return nil })
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--os", "linux"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "Linux host") {
		t.Fatalf("err = %v, want a Linux host hint", err)
	}
}
