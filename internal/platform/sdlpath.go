//go:build !js

package platform

import (
	"os"
	"path/filepath"
	"runtime"

	sdl "github.com/Zyko0/go-sdl3/sdl"
)

// sdlLibraryPath returns the path of the SDL3 shared library for the dynamic
// loader. The loader does not search every install prefix: on macOS it never
// looks in /opt/homebrew/lib, so passing the bare "libSDL3.dylib" fails even
// when Homebrew installed SDL3. goi doctor already knows the usual places, so
// the runtime loader walks the same directories and hands over an absolute
// path. GOI_SDL3_PATH overrides the search for a custom prefix.
func sdlLibraryPath() string {
	if path := os.Getenv("GOI_SDL3_PATH"); path != "" && isSDLFile(path) {
		return path
	}
	var dirs []string
	addEnv := func(key string) {
		if value := os.Getenv(key); value != "" {
			dirs = append(dirs, filepath.SplitList(value)...)
		}
	}
	switch runtime.GOOS {
	case "darwin":
		addEnv("DYLD_LIBRARY_PATH")
		addEnv("DYLD_FALLBACK_LIBRARY_PATH")
		dirs = append(dirs, "/opt/homebrew/lib", "/usr/local/lib", "/usr/lib",
			"/Library/Frameworks/SDL3.framework/Versions/A")
	case "linux", "freebsd":
		addEnv("LD_LIBRARY_PATH")
		dirs = append(dirs, "/usr/lib", "/usr/lib64", "/usr/local/lib", "/usr/local/lib64",
			"/lib", "/lib64",
			"/usr/lib/x86_64-linux-gnu", "/usr/lib/aarch64-linux-gnu",
			"/lib/x86_64-linux-gnu", "/lib/aarch64-linux-gnu")
	case "windows":
		if root := os.Getenv("SystemRoot"); root != "" {
			dirs = append(dirs, filepath.Join(root, "System32"))
		}
	}
	// A packaged app ships the DLL or dylib next to the executable.
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	names := sdlLibraryNames()
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		for _, name := range names {
			path := filepath.Join(dir, name)
			if isSDLFile(path) {
				return path
			}
		}
	}
	// Let the loader try its own search, and report the missing library by name.
	return sdl.Path()
}

func sdlLibraryNames() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"SDL3.dll"}
	case "darwin":
		return []string{"libSDL3.0.dylib", "libSDL3.dylib", "SDL3"}
	default:
		return []string{"libSDL3.so.0", "libSDL3.so"}
	}
}

func isSDLFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
