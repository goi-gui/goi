//go:build !js && !wasip1

package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/ulikunitz/xz"
)

// nsisVersion is the NSIS release goi unpacks when makensis is not installed.
const nsisVersion = "3.11"

// toolCacheDir is where goi keeps the tools it downloaded, next to the SDL3
// and AppImage caches.
func toolCacheDir() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	return filepath.Join(cache, "goi", "tools")
}

// resolveMakensis finds the NSIS compiler: the flag, the environment, PATH,
// then a downloaded copy. The extra environment names NSISDIR for a copy that
// was unpacked from a package instead of installed.
func resolveMakensis(override string, stdout io.Writer) (string, []string, error) {
	if override != "" {
		return override, nil, nil
	}
	if p := os.Getenv("GOI_MAKENSIS"); p != "" {
		return p, nil, nil
	}
	if p, err := exec.LookPath("makensis"); err == nil {
		return p, nil, nil
	}
	return downloadNSIS(stdout)
}

// nsisDeb is one Debian package holding part of NSIS.
type nsisDeb struct {
	name, url, sha256 string
}

// nsisDebs are the pinned Debian builds: nsis carries the binary, nsis-common
// the stubs, plugins and includes makensis reads at run time.
var nsisDebs = map[string]struct {
	tool, common nsisDeb
}{
	"amd64": {
		tool:   nsisDeb{"nsis_3.11-1_amd64.deb", "https://deb.debian.org/debian/pool/main/n/nsis/nsis_3.11-1_amd64.deb", "e6eb6ffb70f0df09e95dedb2d2cf55ad3c2679f7c2ae357d0c00cc4c83178690"},
		common: nsisDeb{"nsis-common_3.11-1_all.deb", "https://deb.debian.org/debian/pool/main/n/nsis/nsis-common_3.11-1_all.deb", "103a3284c1a5356efa0aba90fdfc391c3cde8e7df8726377cef0f98471584047"},
	},
	"arm64": {
		tool:   nsisDeb{"nsis_3.11-1_arm64.deb", "https://deb.debian.org/debian/pool/main/n/nsis/nsis_3.11-1_arm64.deb", "1c3e1f6f045b9269c73c18006eb02bd914949619f1cf709a53450065bb3006e2"},
		common: nsisDeb{"nsis-common_3.11-1_all.deb", "https://deb.debian.org/debian/pool/main/n/nsis/nsis-common_3.11-1_all.deb", "103a3284c1a5356efa0aba90fdfc391c3cde8e7df8726377cef0f98471584047"},
	},
}

// nsisZip is the portable Windows build of NSIS.
var nsisZip = nsisDeb{
	name:   "nsis-" + nsisVersion + ".zip",
	url:    "https://downloads.sourceforge.net/project/nsis/NSIS%203/3.11/nsis-3.11.zip",
	sha256: "c7d27f780ddb6cffb4730138cd1591e841f4b7edb155856901cdf5f214394fa1",
}

// nsisCache is where an unpacked NSIS lives on this host.
func nsisCache() (exe, dir string) {
	root := filepath.Join(toolCacheDir(), "nsis-"+nsisVersion, runtime.GOOS+"-"+runtime.GOARCH)
	switch runtime.GOOS {
	case "windows":
		dir = filepath.Join(root, "nsis-"+nsisVersion)
		return filepath.Join(dir, "Bin", "makensis.exe"), dir
	default:
		dir = filepath.Join(root, "usr", "share", "nsis")
		return filepath.Join(root, "usr", "bin", "makensis"), dir
	}
}

// cachedNSIS reports whether a usable download is already unpacked.
func cachedNSIS() (string, []string, bool) {
	exe, dir := nsisCache()
	if !fileExists(exe) {
		return "", nil, false
	}
	if !dirExists(filepath.Join(dir, "Stubs")) {
		return "", nil, false
	}
	return exe, []string{"NSISDIR=" + dir}, true
}

// downloadNSIS unpacks the pinned NSIS build for this host and returns the
// compiler with the environment it needs.
func downloadNSIS(stdout io.Writer) (string, []string, error) {
	if exe, env, ok := cachedNSIS(); ok {
		return exe, env, nil
	}
	exe, _ := nsisCache()
	root := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // used only by the deb path
	work, err := os.MkdirTemp("", "goi-nsis-*")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(work)
	switch runtime.GOOS {
	case "linux":
		deb, ok := nsisDebs[runtime.GOARCH]
		if !ok {
			return "", nil, fmt.Errorf("no NSIS download for linux/%s; install it or pass --makensis", runtime.GOARCH)
		}
		for _, pkg := range []nsisDeb{deb.tool, deb.common} {
			archive := filepath.Join(work, pkg.name)
			fmt.Fprintf(stdout, "goi: downloading NSIS %s\n", nsisVersion)
			if err := downloadFile(pkg.url, archive, pkg.sha256); err != nil {
				return "", nil, fmt.Errorf("download NSIS: %w (install makensis or pass --makensis)", err)
			}
			if err := extractDebData(archive, root); err != nil {
				return "", nil, fmt.Errorf("unpack NSIS: %w", err)
			}
		}
		if err := os.Chmod(exe, 0o755); err != nil {
			return "", nil, err
		}
	case "windows":
		archive := filepath.Join(work, nsisZip.name)
		fmt.Fprintf(stdout, "goi: downloading NSIS %s\n", nsisVersion)
		if err := downloadFile(nsisZip.url, archive, nsisZip.sha256); err != nil {
			return "", nil, fmt.Errorf("download NSIS: %w (install makensis or pass --makensis)", err)
		}
		if err := extractZipDir(archive, "nsis-"+nsisVersion+"/", filepath.Dir(filepath.Dir(exe))); err != nil {
			return "", nil, fmt.Errorf("unpack NSIS: %w", err)
		}
	default:
		return "", nil, errors.New("NSIS is not bundled for macOS; install it (port install nsis) or pass --makensis")
	}
	if exe, env, ok := cachedNSIS(); ok {
		return exe, env, nil
	}
	return "", nil, errors.New("NSIS was unpacked but makensis is missing")
}

func fileExists(name string) bool {
	info, err := os.Stat(name)
	return err == nil && !info.IsDir()
}

func dirExists(name string) bool {
	info, err := os.Stat(name)
	return err == nil && info.IsDir()
}

// extractDebData unpacks usr/bin/makensis and usr/share/nsis from the data
// member of a .deb into root.
func extractDebData(archive, root string) error {
	return readArMember(archive, "data.tar.xz", func(r io.Reader) error {
		xr, err := xz.NewReader(r)
		if err != nil {
			return err
		}
		tr := tar.NewReader(xr)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			name := strings.TrimPrefix(strings.TrimPrefix(h.Name, "./"), "/")
			keep := name == "usr/bin/makensis" || strings.HasPrefix(name, "usr/share/nsis/")
			if !keep {
				continue
			}
			dst := filepath.Join(root, filepath.FromSlash(name))
			switch h.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(dst, 0o755); err != nil {
					return err
				}
			case tar.TypeReg:
				if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
					return err
				}
				mode := h.FileInfo().Mode().Perm() | 0o200
				out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
				if err != nil {
					return err
				}
				if _, err := io.Copy(out, tr); err != nil {
					out.Close()
					return err
				}
				if err := out.Close(); err != nil {
					return err
				}
			}
		}
	})
}

// readArMember hands the named member of a Unix ar archive (a .deb) to fn.
// The format is a fixed 60-byte header per member; names end in "/" in the
// GNU variant.
func readArMember(archive, want string, fn func(io.Reader) error) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	magic := make([]byte, 8)
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "!<arch>\n" {
		return errors.New("not an ar archive")
	}
	hdr := make([]byte, 60)
	for {
		if _, err := io.ReadFull(r, hdr); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if string(hdr[58:60]) != "`\n" {
			return errors.New("bad ar member header")
		}
		name := strings.TrimSuffix(strings.TrimSpace(string(hdr[0:16])), "/")
		size, err := strconv.Atoi(strings.TrimSpace(string(hdr[48:58])))
		if err != nil || size < 0 {
			return errors.New("bad ar member size")
		}
		if name == want {
			return fn(io.LimitReader(r, int64(size)))
		}
		if _, err := io.CopyN(io.Discard, r, int64(size)); err != nil {
			return err
		}
		if size%2 == 1 {
			if _, err := r.ReadByte(); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("%s: %s not found", filepath.Base(archive), want)
}

// extractZipDir unpacks every entry under prefix into root.
func extractZipDir(archive, prefix, root string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		if rel == "" {
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := f.Mode().Perm() | 0o200
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		rc.Close()
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}
