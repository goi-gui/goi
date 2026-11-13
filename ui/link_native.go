//go:build !js

package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// OpenLink opens a link the way the desktop would. A URL or a deep link goes to
// whatever the system registered for that scheme, which is the user's browser
// for the web; a path that exists opens in the file manager, where the system
// can select it. It returns at once: the opened program runs on its own.
//
// A link a RichText or Text draws is opened this way already, so an app only
// needs this to open a target of its own, such as one an OnLink handler routed.
func OpenLink(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("ui: OpenLink: empty target")
	}
	path, ok := localPath(target)
	if !ok {
		name, args := launchCommand(runtime.GOOS, target)
		return runDetached(name, args...)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("ui: OpenLink: %w", err)
	}
	name, args := revealCommand(runtime.GOOS, path, fi.IsDir())
	return runDetached(name, args...)
}

var (
	// schemeRe matches a URL scheme: `https:`, `mailto:tech@goi.dev`,
	// `myapp://open`.
	schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*:`)
	// driveRe matches a Windows drive, which looks like a scheme but is a path.
	driveRe = regexp.MustCompile(`^[a-zA-Z]:[\\/]`)
)

// localPath reports whether a link names something on this machine, and returns
// its absolute path. A link with a scheme is never asked of the filesystem, so
// `https://goi.dev/a` cannot be mistaken for a path; `file://` is one, and `~`
// is the user's home.
func localPath(target string) (string, bool) {
	s := target
	if rest, ok := strings.CutPrefix(s, "file://"); ok {
		s = rest
	} else if schemeRe.MatchString(s) && !driveRe.MatchString(s) {
		return "", false
	}
	if s == "" {
		return "", false
	}
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		s = filepath.Join(home, s[1:])
	}
	abs, err := filepath.Abs(s)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(abs); err != nil {
		return "", false
	}
	return abs, true
}

// launchCommand is the program that hands a target to the system's own handler.
func launchCommand(goos, target string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{target}
	case "windows":
		// rundll32 hands the target to ShellExecute, which is what a
		// double-click does, so a URL and a registered deep link both work.
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}
	}
	// The freedesktop opener knows URLs, deep links and folders alike.
	return "xdg-open", []string{target}
}

// revealCommand shows a path in the file manager. Finder and Explorer can select
// the item itself; xdg-open has no way to say that, so Linux shows the folder
// that holds it.
func revealCommand(goos, path string, isDir bool) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{"-R", path}
	case "windows":
		return "explorer", []string{"/select," + path}
	}
	if !isDir {
		path = filepath.Dir(path)
	}
	return "xdg-open", []string{path}
}

// runDetached starts a program and returns; the child is reaped in the
// background so a long-lived app does not leave a zombie per link.
func runDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ui: OpenLink: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
