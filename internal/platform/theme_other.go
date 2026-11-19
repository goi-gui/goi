//go:build !linux || js

package platform

// startThemeWatcher is a no-op where the platform reports theme changes itself.
func startThemeWatcher() {}

// stopThemeWatcher is a no-op where no watcher runs.
func stopThemeWatcher() {}

// portalTheme has no freedesktop portal to read.
func portalTheme() Theme { return ThemeUnknown }
