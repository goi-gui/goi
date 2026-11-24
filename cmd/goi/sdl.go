//go:build !js && !wasip1

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// sdl3Version is the SDL runtime goi downloads for a package built away from
// the target. The release assets are pinned by hash.
const sdl3Version = "3.4.20"

const sdl3Base = "https://github.com/libsdl-org/SDL/releases/download/release-" + sdl3Version + "/"

// sdlAsset is one downloadable SDL3 library. member is the path inside the
// archive as 7-Zip sees it; mountMember is the same file under a mounted dmg.
type sdlAsset struct {
	name        string
	url         string
	sha256      string
	member      string
	mountMember string
}

// sdlMacOS is the universal framework dylib, so one download serves both Mac
// architectures.
var sdlMacOS = sdlAsset{
	name:        "SDL3-" + sdl3Version + ".dmg",
	url:         sdl3Base + "SDL3-" + sdl3Version + ".dmg",
	sha256:      "36b7e4cefcf7d55d227b3df5e686f8f33ab34193173a976445d1b57c80009e44",
	member:      "SDL3/SDL3.xcframework/macos-arm64_x86_64/SDL3.framework/Versions/A/SDL3",
	mountMember: "SDL3.xcframework/macos-arm64_x86_64/SDL3.framework/Versions/A/SDL3",
}

// sdlAssets covers the platforms SDL publishes binaries for. Linux has no
// official download: its library comes from the machine that builds the
// package, so the end user still never installs it.
var sdlAssets = map[string]sdlAsset{
	"windows/amd64": {
		name:   "SDL3-" + sdl3Version + "-win32-x64.zip",
		url:    sdl3Base + "SDL3-" + sdl3Version + "-win32-x64.zip",
		sha256: "368bed8b24e44ad9ea372990814f2343fcfb9e468abb7f8f4a3bc146c7304752",
		member: "SDL3.dll",
	},
	"windows/arm64": {
		name:   "SDL3-" + sdl3Version + "-win32-arm64.zip",
		url:    sdl3Base + "SDL3-" + sdl3Version + "-win32-arm64.zip",
		sha256: "51c292eafc114ea3c63519fa2f5413a486979769d5444a636296f8a61c737267",
		member: "SDL3.dll",
	},
	"windows/386": {
		name:   "SDL3-" + sdl3Version + "-win32-x86.zip",
		url:    sdl3Base + "SDL3-" + sdl3Version + "-win32-x86.zip",
		sha256: "f6fbf5afa42195a01b1da8adf25567377d0e212f4b2b6770923903eb3feea254",
		member: "SDL3.dll",
	},
	"darwin/amd64": sdlMacOS,
	"darwin/arm64": sdlMacOS,
}

// sdlBottles are the Linux builds of SDL3. SDL publishes no Linux binary, so
// these are Homebrew's bottles: a tarball of the same release, self-contained
// apart from libc. The sha256 is the OCI blob digest, so the download is
// verified against the digest that names it.
var sdlBottles = map[string]struct {
	digest  string
	machine uint16 // ELF e_machine: 62 is x86-64, 183 is AArch64
}{
	"amd64": {"21efc3443f4dd34b94506c85d53d2c20bbf206f0779ac458aafa5b8551c28ee6", 62},
	"arm64": {"5b6cf2966f7e42e8c1a4d7639d6cf4b7a79adf501b2a798f4a49049f6865e799", 183},
}

//go:embed SDL3-LICENSE.txt
var sdlLicense []byte

// resolvePackSDL returns the SDL3 library to bundle. Linux takes the library
// installed on this machine; Windows and macOS download the pinned SDL release
// for the target, so the end user never installs SDL.
func resolvePackSDL(opts packOptions, stdout io.Writer) (string, error) {
	if opts.NoSDL {
		return "", nil
	}
	if opts.SDL != "" {
		p := resolvePath(".", opts.SDL)
		info, err := os.Stat(p)
		if err != nil {
			return "", fmt.Errorf("--sdl %s: %w", opts.SDL, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("--sdl %s is a directory", opts.SDL)
		}
		return p, nil
	}
	if opts.OS == "linux" {
		// Always the pinned build for the target architecture: the package must
		// not depend on what this machine happens to have installed.
		return downloadLinuxSDL(opts.Arch, stdout)
	}
	return downloadSDL(opts.OS, opts.Arch, stdout)
}

// packSDLNames are the library names an OS's dynamic loader tries.
func packSDLNames(goos string) []string {
	switch goos {
	case "windows":
		return []string{"SDL3.dll"}
	case "darwin":
		return []string{"libSDL3.0.dylib", "libSDL3.dylib", "SDL3"}
	default:
		return []string{"libSDL3.so.0", "libSDL3.so"}
	}
}

// packSDLFile is the name the bundled library takes inside the package.
func packSDLFile(goos string) string { return packSDLNames(goos)[0] }

// downloadSDL fetches and extracts the pinned SDL3 library for one target, and
// caches it next to the downloaded tools.
func downloadSDL(goos, goarch string, stdout io.Writer) (string, error) {
	asset, ok := sdlAssets[goos+"/"+goarch]
	if !ok {
		return "", fmt.Errorf("no SDL3 binary for %s/%s; pass --sdl PATH (or --no-sdl)", goos, goarch)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	// The macOS dylib is universal, so both architectures share one copy.
	archDir := goarch
	if goos == "darwin" {
		archDir = "universal"
	}
	dir := filepath.Join(cache, "goi", "tools", "sdl3-"+sdl3Version, goos+"-"+archDir)
	lib := filepath.Join(dir, packSDLFile(goos))
	if info, err := os.Stat(lib); err == nil && info.Size() > 0 {
		return lib, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp("", "goi-sdl-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	fmt.Fprintf(stdout, "goi: downloading SDL3 %s for %s/%s\n", sdl3Version, goos, goarch)
	archive := filepath.Join(work, asset.name)
	if err := downloadFile(asset.url, archive, asset.sha256); err != nil {
		return "", fmt.Errorf("download SDL3 %s: %w (pass --sdl PATH to use a local library)", sdl3Version, err)
	}
	extracted, err := extractSDL(archive, asset, work)
	if err != nil {
		return "", err
	}
	tmp := lib + ".tmp"
	if err := copyFile(extracted, tmp, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, lib); err != nil {
		return "", err
	}
	return lib, nil
}

// downloadLinuxSDL fetches the Linux build for one architecture. Without a
// local library it is also what a host-targeted pack uses, so the machine does
// not have to install SDL at all.
func downloadLinuxSDL(goarch string, stdout io.Writer) (string, error) {
	bottle, ok := sdlBottles[goarch]
	if !ok {
		return "", fmt.Errorf("no SDL3 binary for linux/%s; pass --sdl PATH (or --no-sdl)", goarch)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	dir := filepath.Join(cache, "goi", "tools", "sdl3-"+sdl3Version, "linux-"+goarch)
	lib := filepath.Join(dir, packSDLFile("linux"))
	if info, err := os.Stat(lib); err == nil && info.Size() > 0 {
		return lib, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp("", "goi-sdl-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	fmt.Fprintf(stdout, "goi: downloading SDL3 %s for linux/%s\n", sdl3Version, goarch)
	token, err := ghcrToken()
	if err != nil {
		return "", fmt.Errorf("download SDL3 %s: %w (pass --sdl PATH to use a local library)", sdl3Version, err)
	}
	archive := filepath.Join(work, "sdl3.tar.gz")
	url := "https://ghcr.io/v2/homebrew/core/sdl3/blobs/sha256:" + bottle.digest
	if err := downloadFileAuth(url, archive, bottle.digest, token); err != nil {
		return "", fmt.Errorf("download SDL3 %s: %w (pass --sdl PATH to use a local library)", sdl3Version, err)
	}
	extracted, err := extractBottledSDL(archive, bottle.machine, work)
	if err != nil {
		return "", err
	}
	tmp := lib + ".tmp"
	if err := copyFile(extracted, tmp, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, lib); err != nil {
		return "", err
	}
	return lib, nil
}

// ghcrToken asks the container registry for an anonymous pull token.
func ghcrToken() (string, error) {
	const tokenURL = "https://ghcr.io/token?service=ghcr.io&scope=repository:homebrew/core/sdl3:pull"
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Get(tokenURL)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token: %s", res.Status)
	}
	var v struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		return "", err
	}
	if v.Token == "" {
		return "", errors.New("token: empty response")
	}
	return v.Token, nil
}

// extractBottledSDL pulls libSDL3.so.0 out of a Homebrew bottle and checks that
// it is an ELF library for the target architecture.
func extractBottledSDL(archive string, machine uint16, work string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(h.Name)
		if base != "libSDL3.so.0" && !strings.HasPrefix(base, "libSDL3.so.0.") {
			continue
		}
		dst := filepath.Join(work, packSDLFile("linux"))
		out, err := os.Create(dst)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return "", err
		}
		if err := out.Close(); err != nil {
			return "", err
		}
		if err := checkELFMachine(dst, machine); err != nil {
			return "", err
		}
		return dst, nil
	}
	return "", errors.New("SDL3 bottle has no libSDL3.so.0")
}

// checkELFMachine rejects a library built for another architecture, which would
// only fail once the packaged app tries to load it.
func checkELFMachine(path string, machine uint16) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(b) < 20 || b[0] != 0x7f || b[1] != 'E' || b[2] != 'L' || b[3] != 'F' {
		return errors.New("SDL3 download is not an ELF library")
	}
	if got := binary.LittleEndian.Uint16(b[18:20]); got != machine {
		return fmt.Errorf("SDL3 download is for ELF machine %d, want %d", got, machine)
	}
	return nil
}

// extractSDL pulls the library out of the target's release archive.
func extractSDL(archive string, a sdlAsset, work string) (string, error) {
	if strings.HasSuffix(a.name, ".zip") {
		return extractZipMember(archive, a.member, work)
	}
	return extractDmgMember(archive, a, work)
}

// extractZipMember copies one file out of a zip into work.
func extractZipMember(archive, member, work string) (string, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != member {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		dst := filepath.Join(work, filepath.Base(member))
		out, err := os.Create(dst)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return "", err
		}
		return dst, out.Close()
	}
	return "", fmt.Errorf("%s: %s not found", filepath.Base(archive), member)
}

// extractDmgMember pulls the framework dylib out of SDL's dmg. macOS mounts the
// image; elsewhere 7-Zip reads the HFS+ volume directly.
func extractDmgMember(archive string, a sdlAsset, work string) (string, error) {
	if runtime.GOOS == "darwin" {
		if p, err := hdiutilExtract(archive, a.mountMember, work); err == nil {
			return p, nil
		}
	}
	for _, tool := range []string{"7z", "7zz", "7za"} {
		bin, err := exec.LookPath(tool)
		if err != nil {
			continue
		}
		out := filepath.Join(work, "dmg")
		if err := os.MkdirAll(out, 0o755); err != nil {
			return "", err
		}
		cmd := exec.Command(bin, "e", "-y", "-o"+out, archive, a.member)
		var why strings.Builder
		cmd.Stderr = &why
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("%s: %w: %s", tool, err, strings.TrimSpace(why.String()))
		}
		return filepath.Join(out, filepath.Base(a.member)), nil
	}
	return "", errors.New("extracting SDL3's macOS dylib needs hdiutil (macOS) or 7-Zip; install 7-Zip or pass --sdl with libSDL3.0.dylib")
}

// hdiutilExtract mounts the dmg and copies the dylib out of the framework.
func hdiutilExtract(archive, member, work string) (string, error) {
	bin, err := exec.LookPath("hdiutil")
	if err != nil {
		return "", err
	}
	mnt := filepath.Join(work, "mnt")
	if err := os.MkdirAll(mnt, 0o755); err != nil {
		return "", err
	}
	if out, err := exec.Command(bin, "attach", "-nobrowse", "-readonly", "-mountpoint", mnt, archive).CombinedOutput(); err != nil {
		return "", fmt.Errorf("hdiutil attach: %w: %s", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command(bin, "detach", mnt).Run()
	dst := filepath.Join(work, filepath.Base(member))
	if err := copyFile(filepath.Join(mnt, member), dst, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// writeSDLLicense writes SDL's zlib notice next to a bundled library.
func writeSDLLicense(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, sdlLicense, 0o644)
}
