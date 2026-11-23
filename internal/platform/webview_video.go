//go:build !js

package platform

// StartVideo starts SDL's video subsystem. It runs after InitWebview so that an
// imported Wayland display can be set before SDL connects its own.
func StartVideo() error { return sdlStartVideo() }
