//go:build !nohotreload

package ui

// defaultHotReload is on for development. goi build adds the nohotreload tag so
// a shipped binary does not watch its design files.
const defaultHotReload = true
