//go:build !nohotreload

package ui

// defaultHotReload is on for a directory design. An embedded design has no
// directory to watch, so a built app skips the watcher without a tag.
const defaultHotReload = true
