//go:build !js && !wasip1

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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
	printDoctorWith(w, latestRelease)
}

func printDoctorWith(w io.Writer, latest func(context.Context) (string, error)) {
	goPath, goRow := goCheck()
	rows := []doctorCheck{
		updateCheck(programVersion(), latest),
		{Name: "OS", Status: "-", Detail: osDetail()},
		goRow,
		cgoCheck(goPath),
		programCheck(),
		installedCheck("goipls", goPath),
		installedCheck("goifmt", goPath),
		sdlCheck(),
		vulkanCheck(),
		appImagePackCheck(),
		nsisCheck(),
		sevenZipCheck(),
		sdlPackCheck(),
		optionalCheck("ffmpeg", "media playback (or set GOI_FFMPEG)"),
		optionalCheck("git", "module downloads without a proxy"),
	}
	printDoctorTable(w, rows)
}

// Doctor colors are adaptive so the table reads in a light or dark terminal.
var (
	doctorAccent = lipgloss.AdaptiveColor{Light: "#5A32A3", Dark: "#C9A9FF"}
	doctorMuted  = lipgloss.AdaptiveColor{Light: "#6C6C6C", Dark: "#8E8E8E"}
	doctorOK     = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#5BD68A"}
	doctorWarn   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#E7C05C"}
	doctorBad    = lipgloss.AdaptiveColor{Light: "#B42318", Dark: "#FF8A80"}
	doctorInfo   = lipgloss.AdaptiveColor{Light: "#0B6BCB", Dark: "#7CC4FF"}
)

// printDoctorTable renders the report: a rounded table with colored statuses,
// which is the part a terminal shows at a glance.
func printDoctorTable(w io.Writer, rows []doctorCheck) {
	r := lipgloss.NewRenderer(w)
	nameW, statusW, detailW := len("COMPONENT"), len("STATUS"), len("DETAIL")
	for _, row := range rows {
		nameW = max(nameW, len(row.Name))
		statusW = max(statusW, len(row.Status))
		detailW = max(detailW, len(row.Detail))
	}
	header := r.NewStyle().Bold(true).Foreground(doctorMuted).
		Render(fmt.Sprintf("%-*s  %-*s  %s", nameW, "COMPONENT", statusW, "STATUS", "DETAIL"))
	rule := r.NewStyle().Foreground(doctorMuted).
		Render(strings.Repeat("─", nameW+statusW+detailW+4))
	lines := []string{header, rule}
	for _, row := range rows {
		status := doctorStatusStyle(r, row.Status).Render(fmt.Sprintf("%-*s", statusW, row.Status))
		lines = append(lines, fmt.Sprintf("%-*s  %s  %s", nameW, row.Name, status, row.Detail))
	}
	table := r.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(doctorAccent).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))

	fmt.Fprintln(w, r.NewStyle().Bold(true).Foreground(doctorAccent).Render("goi doctor"))
	fmt.Fprintln(w, r.NewStyle().Foreground(doctorMuted).Render("build, runtime and packaging requirements"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, table)
	fmt.Fprintln(w)
	footer := r.NewStyle().Foreground(doctorMuted)
	fmt.Fprintln(w, footer.Render("required: Go, SDL3 and a GPU backend (Vulkan on Linux/Windows, Metal on macOS)"))
	fmt.Fprintln(w, footer.Render("missing a tool? goi setup installs goi, goipls and goifmt"))
	fmt.Fprintln(w, footer.Render("goi pack downloads SDL3; appimagetool and NSIS come from this machine"))
}

func doctorStatusStyle(r *lipgloss.Renderer, status string) lipgloss.Style {
	switch status {
	case "ok", "cached":
		return r.NewStyle().Foreground(doctorOK)
	case "download":
		return r.NewStyle().Foreground(doctorInfo)
	case "available":
		return r.NewStyle().Foreground(doctorWarn)
	case "missing":
		return r.NewStyle().Foreground(doctorBad)
	default:
		return r.NewStyle().Foreground(doctorMuted)
	}
}

// appImagePackCheck reports the AppImage packer goi pack runs from this machine.
func appImagePackCheck() doctorCheck {
	if runtime.GOOS != "linux" {
		return doctorCheck{Name: "appimagetool", Status: "n/a", Detail: "AppImage target needs Linux"}
	}
	if p := os.Getenv("GOI_APPIMAGETOOL"); p != "" {
		return doctorCheck{Name: "appimagetool", Status: "ok", Detail: p + " (GOI_APPIMAGETOOL)"}
	}
	if p, err := exec.LookPath("appimagetool"); err == nil {
		return doctorCheck{Name: "appimagetool", Status: "ok", Detail: p}
	}
	return doctorCheck{Name: "appimagetool", Status: "missing", Detail: "needed for a Linux AppImage (or pass --appimagetool)"}
}

// nsisCheck reports the Windows installer compiler goi pack runs.
func nsisCheck() doctorCheck {
	if p := os.Getenv("GOI_MAKENSIS"); p != "" {
		return doctorCheck{Name: "NSIS", Status: "ok", Detail: p + " (GOI_MAKENSIS)"}
	}
	if p, err := exec.LookPath("makensis"); err == nil {
		return doctorCheck{Name: "NSIS", Status: "ok", Detail: p}
	}
	return doctorCheck{Name: "NSIS", Status: "missing", Detail: "needed for a Windows installer (apt/port/winget install nsis, or --makensis)"}
}

// sevenZipCheck reports the unpacker for SDL's macOS disk image away from a Mac.
func sevenZipCheck() doctorCheck {
	if runtime.GOOS == "darwin" {
		return doctorCheck{Name: "7-Zip", Status: "n/a", Detail: "hdiutil reads the macOS disk image"}
	}
	for _, name := range []string{"7z", "7zz", "7za"} {
		if p, err := exec.LookPath(name); err == nil {
			return doctorCheck{Name: "7-Zip", Status: "ok", Detail: p}
		}
	}
	return doctorCheck{Name: "7-Zip", Status: "optional", Detail: "unpacks the macOS SDL3 dmg on this host"}
}

// sdlPackCheck reports the SDL3 build a package ships.
func sdlPackCheck() doctorCheck {
	if fileExists(sdlCachePath(runtime.GOOS, runtime.GOARCH)) {
		return doctorCheck{Name: "SDL3 packages", Status: "cached", Detail: "v" + sdl3Version + " for " + runtime.GOOS + "/" + runtime.GOARCH}
	}
	return doctorCheck{Name: "SDL3 packages", Status: "download", Detail: "v" + sdl3Version + ", installs on the first goi pack"}
}

const releasesURL = "https://api.github.com/repos/goi-gui/goi/releases/latest"

func updateCheck(current string, latest func(context.Context) (string, error)) doctorCheck {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	tag, err := latest(ctx)
	if err != nil {
		return doctorCheck{Name: "Update", Status: "?", Detail: "could not check GitHub releases"}
	}
	if _, ok := versionParts(current); !ok {
		return doctorCheck{Name: "Update", Status: "devel", Detail: "latest is " + tag + "; this is a local build"}
	}
	if compareVersions(tag, current) > 0 {
		return doctorCheck{Name: "Update", Status: "available", Detail: tag + " is newer; run goi update"}
	}
	return doctorCheck{Name: "Update", Status: "ok", Detail: "latest is " + tag}
}

// latestRelease reads the tag of the newest published GitHub release.
func latestRelease(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "goi-doctor")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", errors.New("release has no tag")
	}
	return release.TagName, nil
}

// versionParts accepts a release version such as v0.29.2. A pseudo-version or a
// local +dirty build is not a release and does not compare.
func versionParts(version string) ([3]int, bool) {
	var out [3]int
	version = strings.TrimPrefix(version, "v")
	if version == "" || strings.ContainsAny(version, "-+") {
		return out, false
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func compareVersions(a, b string) int {
	pa, oka := versionParts(a)
	pb, okb := versionParts(b)
	if !oka || !okb {
		return 0
	}
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
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
