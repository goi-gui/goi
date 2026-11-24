//go:build !js && !wasip1

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/goi-gui/goi/internal/img"
)

// packDarwin builds the binary and writes a .app bundle. The bundle is plain
// files, so it is assembled on any host; only the ad-hoc signature needs macOS.
func packDarwin(dir string, opts packOptions, cfg config, icon, sdl string, goRun runFunc, runTool packExecFunc, stdout, stderr io.Writer) (string, error) {
	binary, err := buildProject(dir, "darwin", opts.Arch, goRun, stdout, stderr)
	if err != nil {
		return "", err
	}
	app := filepath.Join(dir, "dist", "darwin-"+opts.Arch, cfg.Name+".app")
	// A rebuild replaces the bundle instead of merging with the old one.
	if err := os.RemoveAll(app); err != nil {
		return "", err
	}
	macos := filepath.Join(app, "Contents", "MacOS")
	resources := filepath.Join(app, "Contents", "Resources")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(resources, 0o755); err != nil {
		return "", err
	}
	if err := copyFile(binary, filepath.Join(macos, cfg.Name), 0o755); err != nil {
		return "", err
	}
	if sdl != "" {
		real, err := filepath.EvalSymlinks(sdl)
		if err != nil {
			return "", err
		}
		// Next to the executable, which is where sdlLibraryPath looks for a
		// packaged dylib.
		if err := copyFile(real, filepath.Join(macos, packSDLFile("darwin")), 0o644); err != nil {
			return "", err
		}
		if err := writeSDLLicense(filepath.Join(resources, "SDL3-LICENSE.txt")); err != nil {
			return "", err
		}
	}
	if err := writeICNS(icon, filepath.Join(resources, cfg.ID+".icns")); err != nil {
		return "", fmt.Errorf("icon: %w", err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(infoPlist(cfg, opts.Version)), 0o644); err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		// Apple silicon refuses to launch a bundle without at least an ad-hoc
		// signature. Distribution still needs a real identity and notarization.
		if err := runTool("codesign", []string{"--force", "--sign", "-", app}, os.Environ(), stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "goi: warning: codesign: %v\n", err)
		}
	} else {
		fmt.Fprintln(stderr, "goi: warning: the .app is unsigned; sign it on a Mac (codesign --force --sign -) before Gatekeeper opens it")
	}
	return app, nil
}

// infoPlist is the bundle metadata Finder and LaunchServices read.
func infoPlist(cfg config, version string) string {
	short, build := "1.0", "1"
	if version != "" {
		short = version
		build = bundleBuildVersion(version)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>%s</string>
	<key>CFBundleDisplayName</key>
	<string>%s</string>
	<key>CFBundleIdentifier</key>
	<string>%s</string>
	<key>CFBundleExecutable</key>
	<string>%s</string>
	<key>CFBundleIconFile</key>
	<string>%s</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleShortVersionString</key>
	<string>%s</string>
	<key>CFBundleVersion</key>
	<string>%s</string>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
`, cfg.Name, cfg.Name, cfg.ID, cfg.Name, cfg.ID, short, build)
}

// bundleBuildVersion is CFBundleVersion: digits and dots only, so a version
// like 1.2.3-beta keeps its numeric part.
func bundleBuildVersion(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' || r == '.' {
			b.WriteRune(r)
			continue
		}
		break
	}
	if out := strings.Trim(b.String(), "."); out != "" {
		return out
	}
	return "1"
}

// icnsSizes are the PNG representations macOS asks for. A type codes one
// point size, and the container simply concatenates them.
var icnsSizes = []struct {
	typ  string
	size int
}{
	{"icp4", 16},
	{"icp5", 32},
	{"icp6", 64},
	{"ic07", 128},
	{"ic08", 256},
	{"ic09", 512},
	{"ic10", 1024},
}

// writeICNS converts the PNG icon into an .icns container. PNG payloads keep
// the conversion in the standard library: no icon tool is needed.
func writeICNS(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	d, err := img.Decode(b)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	maxDim := max(d.W, d.H)
	for _, e := range icnsSizes {
		if e.size > maxDim {
			continue
		}
		payload, err := pngBytes(d, e.size)
		if err != nil {
			return err
		}
		body.WriteString(e.typ)
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(8+len(payload)))
		body.Write(n[:])
		body.Write(payload)
	}
	if body.Len() == 0 {
		// A tiny source matched no size; 256 is the one macOS always uses.
		payload, err := pngBytes(d, 256)
		if err != nil {
			return err
		}
		body.WriteString("ic08")
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(8+len(payload)))
		body.Write(n[:])
		body.Write(payload)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(8+body.Len()))
	out.Write(n[:])
	out.Write(body.Bytes())
	return os.WriteFile(dst, out.Bytes(), 0o644)
}

// pngBytes scales the icon to a square size and encodes it as PNG.
func pngBytes(d *img.Data, size int) ([]byte, error) {
	s := d
	if d.W != size || d.H != size {
		s = img.Scale(d, size, size)
	}
	var buf bytes.Buffer
	imgRGBA := &image.RGBA{Pix: s.Pix, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	if err := png.Encode(&buf, imgRGBA); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
