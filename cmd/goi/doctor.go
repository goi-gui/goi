//go:build !js && !wasip1

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// doctorCheck is one row of the report.
type doctorCheck struct {
	Name   string
	Status string
	Detail string
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "show build and runtime requirements",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			printDoctor(cmd.OutOrStdout())
		},
	}
}

// printDoctor writes the environment report. Missing optional tools do not
// change the exit status: a clean build only needs Go, SDL3 and a GPU backend.
func printDoctor(w io.Writer) {
	goPath, goRow := goCheck()
	rows := []doctorCheck{
		{Name: "OS", Status: "-", Detail: osDetail()},
		goRow,
		cgoCheck(goPath),
		programCheck(),
		installedCheck("goipls", goPath),
		installedCheck("goifmt", goPath),
		sdlCheck(),
		vulkanCheck(),
		optionalCheck("ffmpeg", "media playback (or set GOI_FFMPEG)"),
		optionalCheck("git", "module downloads without a proxy"),
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "COMPONENT\tSTATUS\tDETAIL")
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", row.Name, row.Status, row.Detail)
	}
	tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, "required: Go, SDL3 and a GPU backend (Vulkan on Linux/Windows, Metal on macOS)")
	fmt.Fprintln(w, "missing a tool? goi update refreshes goi, goipls and goifmt")
}

func goCheck() (string, doctorCheck) {
	path, err := exec.LookPath("go")
	if err != nil {
		return "", doctorCheck{Name: "Go", Status: "missing", Detail: "install Go from https://go.dev/dl"}
	}
	out, err := exec.Command(path, "version").Output()
	detail := path
	if err == nil {
		detail = strings.TrimSpace(string(out)) + " (" + path + ")"
	}
	return path, doctorCheck{Name: "Go", Status: "ok", Detail: detail}
}

func cgoCheck(goPath string) doctorCheck {
	if goPath == "" {
		return doctorCheck{Name: "CGO", Status: "-", Detail: "needs Go"}
	}
	out, err := exec.Command(goPath, "env", "CGO_ENABLED").Output()
	if err != nil {
		return doctorCheck{Name: "CGO", Status: "-", Detail: "go env CGO_ENABLED failed"}
	}
	value := strings.TrimSpace(string(out))
	status := "ok"
	if value != "0" {
		status = "note"
	}
	return doctorCheck{Name: "CGO", Status: status, Detail: "CGO_ENABLED=" + value + " (goi builds with 0)"}
}

func programCheck() doctorCheck {
	path, err := os.Executable()
	if err != nil {
		path = "goi"
	}
	return doctorCheck{Name: "goi", Status: "ok", Detail: programVersion() + " (" + path + ")"}
}

func installedCheck(name, goPath string) doctorCheck {
	path, err := exec.LookPath(name)
	if err != nil {
		return doctorCheck{
			Name:   name,
			Status: "missing",
			Detail: "go install github.com/goi-gui/goi/cmd/" + name + "@latest",
		}
	}
	detail := path
	if version := binaryVersion(goPath, path); version != "" {
		detail = version + " (" + path + ")"
	}
	return doctorCheck{Name: name, Status: "ok", Detail: detail}
}

// binaryVersion reads a go install's module version from its build info.
func binaryVersion(goPath, path string) string {
	if goPath == "" {
		return ""
	}
	out, err := exec.Command(goPath, "version", "-m", path).Output()
	if err != nil {
		return ""
	}
	return moduleVersion(string(out))
}

// moduleVersion finds the main module line in `go version -m` output:
//
//	mod github.com/goi-gui/goi v0.28.1 h1:...
func moduleVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "mod" && fields[1] == "github.com/goi-gui/goi" {
			return fields[2]
		}
	}
	return ""
}

func osDetail() string {
	detail := runtime.GOOS + "/" + runtime.GOARCH
	if name := distroName(); name != "" {
		detail += ", " + name
	}
	if kernel := kernelVersion(); kernel != "" {
		detail += ", kernel " + kernel
	}
	return detail
}

func distroName() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if name, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(name, `"`)
		}
	}
	return ""
}

func kernelVersion() string {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return ""
	}
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sdlCheck() doctorCheck {
	path := findLibrary(sdlNames())
	if path == "" {
		return doctorCheck{
			Name:   "SDL3",
			Status: "missing",
			Detail: "install the SDL3 runtime library (libsdl3-0, sdl3, or brew install sdl3)",
		}
	}
	return doctorCheck{Name: "SDL3", Status: "ok", Detail: path}
}

func vulkanCheck() doctorCheck {
	path := findLibrary(vulkanNames())
	icds := vulkanICDs()
	if path != "" {
		detail := path
		if len(icds) > 0 {
			detail += fmt.Sprintf("; %d ICD(s)", len(icds))
		} else {
			detail += "; no ICDs found"
		}
		return doctorCheck{Name: "Vulkan", Status: "ok", Detail: detail}
	}
	if runtime.GOOS == "darwin" {
		return doctorCheck{Name: "Vulkan", Status: "optional", Detail: "Metal is the default; MoltenVK adds Vulkan"}
	}
	return doctorCheck{Name: "Vulkan", Status: "missing", Detail: "install the Vulkan loader and a GPU driver"}
}

func optionalCheck(name, why string) doctorCheck {
	path, err := exec.LookPath(name)
	if err != nil {
		return doctorCheck{Name: name, Status: "optional", Detail: why}
	}
	return doctorCheck{Name: name, Status: "ok", Detail: path}
}

func sdlNames() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"SDL3.dll"}
	case "darwin":
		return []string{"libSDL3.0.dylib", "libSDL3.dylib", "SDL3"}
	default:
		return []string{"libSDL3.so.0", "libSDL3.so"}
	}
}

func vulkanNames() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"vulkan-1.dll"}
	case "darwin":
		return []string{"libvulkan.1.dylib", "libvulkan.dylib", "libMoltenVK.dylib"}
	default:
		return []string{"libvulkan.so.1", "libvulkan.so"}
	}
}

// findLibrary looks in the loader paths and the usual install directories. The
// SDL3 runtime is not on PATH on Linux, so ldconfig is the last resort.
func findLibrary(names []string) string {
	dirs := libraryDirs()
	for _, name := range names {
		for _, dir := range dirs {
			path := filepath.Join(dir, name)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path
			}
		}
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if runtime.GOOS == "linux" {
		return ldconfigLibrary(names)
	}
	return ""
}

func libraryDirs() []string {
	var dirs []string
	addEnv := func(key string) {
		if value := os.Getenv(key); value != "" {
			dirs = append(dirs, filepath.SplitList(value)...)
		}
	}
	addEnv("PATH")
	switch runtime.GOOS {
	case "linux":
		addEnv("LD_LIBRARY_PATH")
		dirs = append(dirs,
			"/usr/lib", "/usr/lib64", "/usr/local/lib", "/usr/local/lib64",
			"/lib", "/lib64",
			"/usr/lib/x86_64-linux-gnu", "/usr/lib/aarch64-linux-gnu",
			"/lib/x86_64-linux-gnu", "/lib/aarch64-linux-gnu",
		)
	case "darwin":
		addEnv("DYLD_LIBRARY_PATH")
		addEnv("DYLD_FALLBACK_LIBRARY_PATH")
		dirs = append(dirs, "/opt/homebrew/lib", "/usr/local/lib", "/usr/lib",
			"/Library/Frameworks/SDL3.framework/Versions/A",
			"/Library/Frameworks/vulkan.framework/Versions/A")
	case "windows":
		if root := os.Getenv("SystemRoot"); root != "" {
			dirs = append(dirs, filepath.Join(root, "System32"))
		}
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	seen := map[string]bool{}
	out := dirs[:0]
	for _, dir := range dirs {
		if dir != "" && !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return out
}

func ldconfigLibrary(names []string) string {
	out, err := exec.Command("ldconfig", "-p").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		for _, name := range names {
			if fields[0] == name {
				return fields[len(fields)-1]
			}
		}
	}
	return ""
}

func vulkanICDs() []string {
	for _, key := range []string{"VK_ICD_FILENAMES", "VK_DRIVER_FILES"} {
		if value := os.Getenv(key); value != "" {
			return existingFiles(filepath.SplitList(value))
		}
	}
	var dirs []string
	switch runtime.GOOS {
	case "linux":
		dirs = []string{"/usr/share/vulkan/icd.d", "/etc/vulkan/icd.d", "/usr/local/share/vulkan/icd.d"}
	case "darwin":
		dirs = []string{"/opt/homebrew/share/vulkan/icd.d", "/usr/local/share/vulkan/icd.d", "/usr/share/vulkan/icd.d"}
	}
	var out []string
	for _, dir := range dirs {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		out = append(out, matches...)
	}
	sort.Strings(out)
	return out
}

func existingFiles(paths []string) []string {
	var out []string
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}
