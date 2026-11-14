//go:build !js

package platform

// event kinds mirror the GOI_EV_* enum in sdl_shim.h.
const (
	_ = iota // evNone: an event the framework does not use
	evQuit
	evClose
	evResize
	evExpose
	evFocusIn
	evFocusOut
	evMouseLeave
	evMotion
	evButton
	evWheel
	evKey
	evText
	evEditing
	evUser
	evTheme
	evLocale
	evPinch
)

// rawEvent is one translated SDL event.
type rawEvent struct {
	kind     int
	width    int
	height   int
	x, y     float64
	button   int
	down     bool
	wheelX   float64
	wheelY   float64
	scancode int
	mod      int
	text     string
	caret    int
	scale    float64
}

// wantWaylandDriver reports whether a Wayland session should get SDL's native
// driver. An explicit SDL_VIDEODRIVER wins, as it does inside SDL.
func wantWaylandDriver(getenv func(string) string) bool {
	return getenv("SDL_VIDEODRIVER") == "" && getenv("WAYLAND_DISPLAY") != ""
}
