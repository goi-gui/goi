//go:build !js && !wasip1

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/png" // the icon check reads the PNG header
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// appimagetoolVersion is the release goi downloads when the tool is not on
// PATH. Its assets are pinned by hash: the tag is fixed, so a file that does
// not match is broken or has been tampered with.
const appimagetoolVersion = "1.9.1"

// appimagetoolAssets is the download for each AppImage architecture. The host
// binary is what runs; the target architecture is passed in ARCH.
var appimagetoolAssets = map[string]struct{ asset, sha256 string }{
	"x86_64":  {"appimagetool-x86_64.AppImage", "ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0"},
	"aarch64": {"appimagetool-aarch64.AppImage", "f0837e7448a0c1e4e650a93bb3e85802546e60654ef287576f46c71c126a9158"},
	"armhf":   {"appimagetool-armhf.AppImage", "42b61cba5495d8aaf418a5c9a015a49b85ad92efabcbd3c341f1540440e4e23d"},
	"i686":    {"appimagetool-i686.AppImage", "7ad9ff47c203aae0149b18f6df9e3018b2e2f470ea644a0413e3ded39e9e3bdb"},
}

// packOptions is everything goi pack reads from the command line.
type packOptions struct {
	OS, Arch string
	Version  string
	Icon     string
	SDL      string
	NoSDL    bool
	Tool     string // appimagetool
	Makensis string
	Keep     bool
}

// packExecFunc runs an external packager: appimagetool, codesign or makensis.
// It is a value so a test can record the invocation instead of running it.
type packExecFunc func(path string, args, env []string, stdout, stderr io.Writer) error

func newPackCmd(goRun runFunc) *cobra.Command {
	opts := packOptions{}
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "package the built app for its operating system",
		Long: "Build the app and write a package for --os (the host by default):\n\n" +
			"  linux    dist/linux-<arch>/<name>-<x86_64|aarch64>.AppImage\n" +
			"  darwin   dist/darwin-<arch>/<name>.app\n" +
			"  windows  dist/windows-<arch>/<name>-<arch>-setup.exe\n\n" +
			"The package holds the binary, the SDL3 runtime, and the icon and identity from\n" +
			"goi.yaml. SDL3 is a pinned release for the target, downloaded once, so the\n" +
			"app never needs it installed. Pass --version to put a version in the file\n" +
			"name and the package metadata. A Linux AppImage is packed on Linux with\n" +
			"appimagetool (--appimagetool, GOI_APPIMAGETOOL, PATH, or a pinned download);\n" +
			"a Windows installer needs NSIS (--makensis, GOI_MAKENSIS or makensis on PATH);\n" +
			"the .app is assembled anywhere and signed on a Mac.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.OS == "linux" && runtime.GOOS != "linux" {
				return errors.New("a Linux AppImage needs a Linux host: appimagetool is a Linux binary; run goi pack on Linux or in a container")
			}
			out, err := packProject(".", opts, goRun, execPackTool, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "goi: packed", out)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.OS, "os", runtime.GOOS, "target operating system (linux, darwin, windows)")
	f.StringVar(&opts.Arch, "arch", runtime.GOARCH, "target architecture (GOARCH)")
	f.StringVar(&opts.Version, "version", "", "package version, shown in the file name and the app metadata")
	f.StringVar(&opts.Icon, "icon", "", "icon PNG (default: icon: in goi.yaml, or assets/icon.png)")
	f.StringVar(&opts.SDL, "sdl", "", "SDL3 library to bundle (default: a pinned release for the target)")
	f.BoolVar(&opts.NoSDL, "no-sdl", false, "do not bundle SDL3; the target system must install it")
	f.StringVar(&opts.Tool, "appimagetool", "", "appimagetool binary (default: PATH, then a pinned download)")
	f.StringVar(&opts.Makensis, "makensis", "", "makensis binary for the Windows installer (default: GOI_MAKENSIS, then PATH)")
	f.BoolVar(&opts.Keep, "keep-appdir", false, "keep the assembled package directory for inspection")
	return cmd
}

// packProject builds one package and returns its path.
func packProject(dir string, opts packOptions, goRun runFunc, runTool packExecFunc, stdout, stderr io.Writer) (string, error) {
	if opts.OS == "" {
		opts.OS = runtime.GOOS
	}
	if opts.Arch == "" {
		opts.Arch = runtime.GOARCH
	}
	switch opts.OS {
	case "linux", "darwin", "windows":
	case "js", "wasip1":
		return "", errors.New("a browser build is not a desktop package; use goi build --os js --arch wasm")
	default:
		return "", fmt.Errorf("packaging for %s is not supported; use linux, darwin or windows", opts.OS)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return "", err
	}
	if err := checkPackIdentity(cfg); err != nil {
		return "", err
	}
	opts.Version = strings.TrimSpace(opts.Version)
	if !validPackageVersion(opts.Version) {
		return "", fmt.Errorf("version %q is not a package version; use letters, digits, '.', '_', '+' or '-'", opts.Version)
	}
	// Everything a package needs is checked before the compile, so a missing
	// icon does not cost a build.
	icon, err := resolvePackIcon(dir, cfg, opts.Icon, stderr)
	if err != nil {
		return "", err
	}
	sdl, err := resolvePackSDL(opts, stdout)
	if err != nil {
		return "", err
	}
	switch opts.OS {
	case "linux":
		return packLinux(dir, opts, cfg, icon, sdl, goRun, runTool, stdout, stderr)
	case "darwin":
		return packDarwin(dir, opts, cfg, icon, sdl, goRun, runTool, stdout, stderr)
	default:
		return packWindows(dir, opts, cfg, icon, sdl, goRun, runTool, stdout, stderr)
	}
}

// appImageArch maps a GOARCH to the architecture name an AppImage uses.
func appImageArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	case "arm":
		return "armhf", nil
	case "386":
		return "i686", nil
	}
	return "", fmt.Errorf("no AppImage architecture for %s", goarch)
}

// validPackageVersion accepts the versions a file name and installer can
// carry. An empty version keeps the old file names and metadata.
func validPackageVersion(v string) bool {
	if v == "" {
		return true
	}
	return appSafeVersion.MatchString(v)
}

// appSafeVersion is a version without spaces, slashes or anything else that
// would need quoting in a desktop entry, plist or NSIS script.
var appSafeVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~-]*$`)

// appSafeName is what a binary, desktop entry and icon may be named. goi build
// accepts any file name; a package also spells them in desktop, plist or
// installer files, which have no quoting of their own.
var appSafeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// checkPackIdentity validates the names a package is built around.
func checkPackIdentity(cfg config) error {
	if cfg.Name == "" {
		return errors.New("goi.yaml has no name; add name: myapp")
	}
	if !appSafeName.MatchString(cfg.Name) {
		return fmt.Errorf("goi.yaml name %q is not a package name; use letters, digits, '.', '_' or '-'", cfg.Name)
	}
	if cfg.ID == "" {
		return errors.New("goi.yaml has no id; add id: com.example.myapp")
	}
	if !appSafeName.MatchString(cfg.ID) {
		return fmt.Errorf("goi.yaml id %q is not a reverse-DNS name; use letters, digits, '.', '_' or '-'", cfg.ID)
	}
	return nil
}

// resolvePackIcon finds the PNG a package shows in a menu. An explicit flag or
// goi.yaml entry wins; otherwise the usual asset names are tried.
func resolvePackIcon(dir string, cfg config, flag string, stderr io.Writer) (string, error) {
	explicit := flag
	if explicit == "" {
		explicit = cfg.Icon
	}
	if explicit != "" {
		p := resolvePath(dir, explicit)
		if err := checkIconPNG(p, stderr); err != nil {
			return "", fmt.Errorf("icon %s: %w", explicit, err)
		}
		return p, nil
	}
	assets := resolvePath(dir, cfg.Assets)
	for _, p := range []string{
		filepath.Join(assets, "icon.png"),
		filepath.Join(assets, "images", "icon.png"),
		filepath.Join(assets, "images", cfg.Name+".png"),
	} {
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			continue
		}
		if err := checkIconPNG(p, stderr); err != nil {
			return "", fmt.Errorf("icon %s: %w", p, err)
		}
		return p, nil
	}
	return "", errors.New("no icon found; add icon: assets/icon.png to goi.yaml (a 256x256 PNG)")
}

// checkIconPNG reads the header and size of an icon, so the packager is not the
// first to say the file is not an image.
func checkIconPNG(path string, stderr io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var sig [8]byte
	if _, err := io.ReadFull(f, sig[:]); err != nil || string(sig[:]) != "\x89PNG\r\n\x1a\n" {
		return errors.New("not a PNG file")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	c, _, err := image.DecodeConfig(f)
	if err != nil {
		return err
	}
	if c.Width < 256 || c.Height < 256 {
		fmt.Fprintf(stderr, "goi: warning: icon %s is %dx%d; 256x256 or larger looks best\n", filepath.Base(path), c.Width, c.Height)
	}
	return nil
}

// copyFile writes src as dst with perm. It streams, because a binary is not a
// file to hold twice in memory.
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// packLinux builds the binary and packs it into an AppImage.
func packLinux(dir string, opts packOptions, cfg config, icon, sdl string, goRun runFunc, runTool packExecFunc, stdout, stderr io.Writer) (string, error) {
	imgArch, err := appImageArch(opts.Arch)
	if err != nil {
		return "", err
	}
	tool, err := packToolPath(opts.Tool, stdout)
	if err != nil {
		return "", err
	}
	binary, err := buildProject(dir, "linux", opts.Arch, goRun, stdout, stderr)
	if err != nil {
		return "", err
	}
	appDir, err := os.MkdirTemp("", "goi-pack-*")
	if err != nil {
		return "", err
	}
	if !opts.Keep {
		defer os.RemoveAll(appDir)
	}
	if err := writeAppDir(appDir, cfg, opts.Version, binary, icon, sdl); err != nil {
		return "", err
	}
	if opts.Keep {
		fmt.Fprintln(stdout, "goi: appdir kept at", appDir)
	}
	image := cfg.Name + "-" + imgArch + ".AppImage"
	if opts.Version != "" {
		image = cfg.Name + "-" + opts.Version + "-" + imgArch + ".AppImage"
	}
	out := filepath.Join(dir, "dist", "linux-"+opts.Arch, image)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	// 1.9.x reads the architecture from ARCH, and the extract-and-run mode
	// keeps a machine without FUSE working.
	env := envWith(os.Environ(), "ARCH", imgArch)
	env = envWith(env, "APPIMAGE_EXTRACT_AND_RUN", "1")
	if opts.Version != "" {
		env = envWith(env, "VERSION", opts.Version)
	}
	if err := runTool(tool, []string{"--no-appstream", appDir, out}, env, stdout, stderr); err != nil {
		return "", fmt.Errorf("appimagetool: %w", err)
	}
	return out, nil
}

// writeAppDir writes the files an AppImage is built from. SDL3 sits next to the
// binary because the runtime loader looks there first.
func writeAppDir(appDir string, cfg config, version, binary, icon, sdl string) error {
	binDir := filepath.Join(appDir, "usr", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if err := copyFile(binary, filepath.Join(binDir, cfg.Name), 0o755); err != nil {
		return err
	}
	if sdl != "" {
		real, err := filepath.EvalSymlinks(sdl)
		if err != nil {
			return err
		}
		if err := copyFile(real, filepath.Join(binDir, "libSDL3.so.0"), 0o644); err != nil {
			return err
		}
		if err := writeSDLLicense(filepath.Join(appDir, "usr", "share", "licenses", cfg.Name, "SDL3-LICENSE.txt")); err != nil {
			return err
		}
	}
	iconName := cfg.ID + ".png"
	if err := copyFile(icon, filepath.Join(appDir, iconName), 0o644); err != nil {
		return err
	}
	// A copy rather than a symlink, so extracting the image works on a
	// filesystem without link support.
	if err := copyFile(icon, filepath.Join(appDir, ".DirIcon"), 0o644); err != nil {
		return err
	}
	desktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Exec=%s
Icon=%s
Terminal=false
Categories=Utility;
`, cfg.Name, cfg.Name, cfg.ID)
	if version != "" {
		desktop += "X-AppImage-Version=" + version + "\n"
	}
	if err := os.WriteFile(filepath.Join(appDir, cfg.ID+".desktop"), []byte(desktop), 0o644); err != nil {
		return err
	}
	appRun := "#!/bin/sh\nHERE=\"${APPDIR:-$(dirname \"$(readlink -f \"$0\")\")}\"\nexec \"$HERE/usr/bin/" + cfg.Name + "\" \"$@\"\n"
	return os.WriteFile(filepath.Join(appDir, "AppRun"), []byte(appRun), 0o755)
}

// packToolPath finds appimagetool: the flag, the environment, PATH, then the
// pinned download.
func packToolPath(override string, stdout io.Writer) (string, error) {
	if override != "" {
		return override, nil
	}
	if p := os.Getenv("GOI_APPIMAGETOOL"); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("appimagetool"); err == nil {
		return p, nil
	}
	return downloadAppimagetool(stdout)
}

// appimagetoolCachePath is the downloaded copy for a host architecture.
func appimagetoolCachePath(hostArch string) string {
	return filepath.Join(toolCacheDir(), "appimagetool-"+appimagetoolVersion+"-"+hostArch+".AppImage")
}

// downloadAppimagetool fetches and verifies the pinned release once, and keeps
// it in the user cache for later packs.
func downloadAppimagetool(stdout io.Writer) (string, error) {
	hostArch, err := appImageArch(runtime.GOARCH)
	if err != nil {
		return "", fmt.Errorf("appimagetool is not on PATH and goi has no download for %s; pass --appimagetool", runtime.GOARCH)
	}
	asset, ok := appimagetoolAssets[hostArch]
	if !ok {
		return "", fmt.Errorf("appimagetool is not on PATH and goi has no download for %s; pass --appimagetool", hostArch)
	}
	tools := filepath.Dir(appimagetoolCachePath(hostArch))
	path := appimagetoolCachePath(hostArch)
	if sum, err := fileSHA256(path); err == nil && strings.EqualFold(sum, asset.sha256) {
		return path, nil
	}
	if err := os.MkdirAll(tools, 0o755); err != nil {
		return "", err
	}
	fmt.Fprintln(stdout, "goi: downloading appimagetool", appimagetoolVersion)
	url := "https://github.com/AppImage/appimagetool/releases/download/" + appimagetoolVersion + "/" + asset.asset
	if err := downloadFile(url, path, asset.sha256); err != nil {
		return "", fmt.Errorf("download appimagetool %s: %w (install it and pass --appimagetool)", appimagetoolVersion, err)
	}
	return path, nil
}

// maxToolBytes caps the tool download. The real asset is about 15 MB.
const maxToolBytes = 64 << 20

// downloadFile writes url to dst only when its SHA-256 matches want.
func downloadFile(url, dst, want string) error {
	return downloadFileAuth(url, dst, want, "")
}

// downloadFileAuth is downloadFile with an optional bearer token, which the
// container registry in front of the Linux SDL build needs.
func downloadFileAuth(url, dst, want, bearer string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, res.Status)
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(res.Body, maxToolBytes))
	closeErr := f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	if n >= maxToolBytes {
		os.Remove(tmp)
		return errors.New("download is too large")
	}
	if got := hex.EncodeToString(sum.Sum(nil)); !strings.EqualFold(got, want) {
		os.Remove(tmp)
		return fmt.Errorf("checksum mismatch: got %s", got)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// fileSHA256 is the hash of one file, used to reuse a cached download.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// execPackTool runs an external packager for real.
func execPackTool(path string, args, env []string, stdout, stderr io.Writer) error {
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
