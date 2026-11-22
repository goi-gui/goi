//go:build !js

package ui

// startWebReload is browser-only: a desktop design is watched on disk.
func (a *App) startWebReload() {}
