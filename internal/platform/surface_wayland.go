//go:build linux && !x11

package platform

import (
	"fmt"
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// SurfaceHandles returns the Wayland display and surface pointers for WebGPU.
func (p *Window) SurfaceHandles() (display, window uintptr, kind SurfaceKind, err error) {
	if glfw.GetPlatform() != glfw.PlatformWayland {
		return 0, 0, 0, fmt.Errorf("platform: built for wayland but GLFW selected platform %d (rebuild with -tags x11)", glfw.GetPlatform())
	}
	d := glfw.GetWaylandDisplay()
	s := p.win.GetWaylandWindow()
	if d == nil || s == nil {
		return 0, 0, 0, fmt.Errorf("platform: glfw returned nil wayland handles")
	}
	return uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(s)), SurfaceWayland, nil
}

// Clipboard returns the clipboard text.
func Clipboard() string { return glfw.GetClipboardString() }

// SetClipboard replaces the clipboard text.
func SetClipboard(s string) { glfw.SetClipboardString(s) }
