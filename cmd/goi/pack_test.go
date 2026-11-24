//go:build !js && !wasip1

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppImageArch(t *testing.T) {
	cases := map[string]string{
		"amd64": "x86_64",
		"arm64": "aarch64",
		"arm":   "armhf",
		"386":   "i686",
	}
	for goarch, want := range cases {
		got, err := appImageArch(goarch)
		if err != nil || got != want {
			t.Fatalf("appImageArch(%s) = %q, %v; want %q", goarch, got, err, want)
		}
	}
	if _, err := appImageArch("riscv64"); err == nil {
		t.Fatal("riscv64 has no AppImage architecture")
	}
}

func TestPackAppImageToolIsPinned(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64", "armhf", "i686"} {
		asset, ok := appimagetoolAssets[arch]
		if !ok || asset.asset == "" || len(asset.sha256) != 64 {
			t.Fatalf("%s asset = %+v, want a name and a sha256", arch, asset)
		}
	}
}

func writePackIcon(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{R: 30, G: 90, B: 160, A: 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePackIcon(t *testing.T) {
	dir := t.TempDir()
	cfg := config{Name: "myapp", Assets: "assets"}
	icon := filepath.Join(dir, "assets", "images", "icon.png")
	writePackIcon(t, icon, 256, 256)

	got, err := resolvePackIcon(dir, cfg, "", io.Discard)
	if err != nil || got != icon {
		t.Fatalf("default icon = %q, %v; want %q", got, err, icon)
	}

	art := filepath.Join(dir, "art.png")
	writePackIcon(t, art, 256, 256)
	cfg.Icon = "art.png"
	if got, err := resolvePackIcon(dir, cfg, "", io.Discard); err != nil || got != art {
		t.Fatalf("goi.yaml icon = %q, %v; want %q", got, err, art)
	}
	if got, err := resolvePackIcon(dir, cfg, "assets/images/icon.png", io.Discard); err != nil || got != icon {
		t.Fatalf("flag icon = %q, %v; want %q", got, err, icon)
	}

	if _, err := resolvePackIcon(t.TempDir(), config{Name: "other", Assets: "assets"}, "", io.Discard); err == nil || !strings.Contains(err.Error(), "icon") {
		t.Fatalf("missing icon error = %v, want an icon hint", err)
	}

	notPNG := filepath.Join(dir, "icon.png")
	if err := os.WriteFile(notPNG, []byte("GIF89a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePackIcon(dir, config{Name: "myapp", Icon: "icon.png", Assets: "assets"}, "", io.Discard); err == nil || !strings.Contains(err.Error(), "not a PNG") {
		t.Fatalf("non-PNG error = %v, want a PNG hint", err)
	}
}

func TestWriteAppDir(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "app")
	if err := os.WriteFile(binary, []byte("ELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	icon := filepath.Join(dir, "icon.png")
	writePackIcon(t, icon, 256, 256)
	sdl := filepath.Join(dir, "libSDL3.so.0")
	if err := os.WriteFile(sdl, []byte("SDL"), 0o644); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(dir, "AppDir")
	cfg := config{Name: "myapp", ID: "com.example.myapp"}
	if err := writeAppDir(appDir, cfg, binary, icon, sdl); err != nil {
		t.Fatal(err)
	}

	if got, err := os.ReadFile(filepath.Join(appDir, "usr", "bin", "myapp")); err != nil || string(got) != "ELF" {
		t.Fatalf("binary = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(appDir, "usr", "bin", "libSDL3.so.0")); err != nil || string(got) != "SDL" {
		t.Fatalf("SDL3 = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(appDir, "com.example.myapp.png")); err != nil || len(got) == 0 {
		t.Fatalf("icon = %d bytes, %v", len(got), err)
	}
	if info, err := os.Stat(filepath.Join(appDir, ".DirIcon")); err != nil || info.Size() == 0 {
		t.Fatalf(".DirIcon = %v, %v", info, err)
	}
	desktop, err := os.ReadFile(filepath.Join(appDir, "com.example.myapp.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Type=Application", "Name=myapp", "Exec=myapp", "Icon=com.example.myapp", "Terminal=false", "Categories=Utility;"} {
		if !strings.Contains(string(desktop), want) {
			t.Fatalf("desktop entry lacks %q:\n%s", want, desktop)
		}
	}
	appRun, err := os.ReadFile(filepath.Join(appDir, "AppRun"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(appRun), `usr/bin/myapp`) || !strings.Contains(string(appRun), `"$@"`) {
		t.Fatalf("AppRun = %q", appRun)
	}
	if info, err := os.Stat(filepath.Join(appDir, "AppRun")); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("AppRun is not executable: %v", err)
	}
}

func TestPackProject(t *testing.T) {
	dir := buildProjectDir(t, "name: myapp\nid: com.example.myapp\n")
	icon := filepath.Join(dir, "assets", "icon.png")
	writePackIcon(t, icon, 256, 256)

	goRun := func(_ string, args []string, _, _ io.Writer, _ []string) error {
		out := args[len(args)-2]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, []byte("binary"), 0o755)
	}
	var gotArgs, gotEnv []string
	var appDir string
	runTool := func(path string, args, env []string, _, _ io.Writer) error {
		if path != "/fake/appimagetool" {
			t.Fatalf("tool = %q", path)
		}
		gotArgs, gotEnv = args, env
		appDir = args[1]
		return os.WriteFile(args[2], []byte("appimage"), 0o755)
	}
	opts := packOptions{OS: "linux", Arch: "amd64", Tool: "/fake/appimagetool", NoSDL: true, Keep: true}
	out, err := packProject(dir, opts, goRun, runTool, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(appDir)

	wantOut := filepath.Join(dir, "dist", "linux-amd64", "myapp-x86_64.AppImage")
	if out != wantOut {
		t.Fatalf("out = %q, want %q", out, wantOut)
	}
	if gotArgs[0] != "--no-appstream" || gotArgs[1] != appDir || gotArgs[2] != out {
		t.Fatalf("appimagetool args = %v", gotArgs)
	}
	if got := envValue(gotEnv, "ARCH"); got != "x86_64" {
		t.Fatalf("ARCH = %q, want x86_64", got)
	}
	if got := envValue(gotEnv, "APPIMAGE_EXTRACT_AND_RUN"); got != "1" {
		t.Fatalf("APPIMAGE_EXTRACT_AND_RUN = %q, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(appDir, "usr", "bin", "myapp")); err != nil {
		t.Fatalf("AppDir binary missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "usr", "bin", "libSDL3.so.0")); !os.IsNotExist(err) {
		t.Fatalf("--no-sdl still bundled SDL3: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("AppImage missing: %v", err)
	}
}

func TestPackProjectChecksBeforeBuilding(t *testing.T) {
	built := 0
	goRun := func(string, []string, io.Writer, io.Writer, []string) error {
		built++
		return nil
	}
	tool := func(string, []string, []string, io.Writer, io.Writer) error { return nil }

	dir := buildProjectDir(t, "name: myapp\nid: com.example.myapp\n")
	_, err := packProject(dir, packOptions{OS: "linux", Arch: "amd64", NoSDL: true}, goRun, tool, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "icon") {
		t.Fatalf("missing icon error = %v", err)
	}

	writePackIcon(t, filepath.Join(dir, "assets", "icon.png"), 256, 256)
	if _, err := packProject(dir, packOptions{OS: "plan9", Arch: "amd64", NoSDL: true}, goRun, tool, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("plan9 error = %v", err)
	}
	if _, err := packProject(dir, packOptions{OS: "js", Arch: "wasm", NoSDL: true}, goRun, tool, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "browser") {
		t.Fatalf("js error = %v", err)
	}

	dir = buildProjectDir(t, "name: myapp\n")
	writePackIcon(t, filepath.Join(dir, "assets", "icon.png"), 256, 256)
	if _, err := packProject(dir, packOptions{OS: "linux", Arch: "amd64", NoSDL: true}, goRun, tool, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("missing id error = %v", err)
	}

	if built != 0 {
		t.Fatalf("go build ran %d times before the package was valid", built)
	}
}
