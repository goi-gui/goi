//go:build linux && x11

package platform

import (
	"fmt"
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// SurfaceHandles returns the X11 Display and Window for WebGPU.
func (p *Window) SurfaceHandles() (display, window uintptr, kind SurfaceKind, err error) {
	if glfw.GetPlatform() != glfw.PlatformX11 {
		return 0, 0, 0, fmt.Errorf("platform: built with -tags x11 but GLFW selected platform %d", glfw.GetPlatform())
	}
	d := glfw.GetX11Display()
	id := p.win.GetX11Window()
	if d == nil || id == 0 {
		return 0, 0, 0, fmt.Errorf("platform: glfw returned nil x11 handles")
	}
	return uintptr(unsafe.Pointer(d)), uintptr(id), SurfaceX11, nil
}

// Clipboard returns the clipboard text.
func Clipboard() string { return glfw.GetClipboardString() }

// SetClipboard replaces the clipboard text.
func SetClipboard(s string) { glfw.SetClipboardString(s) }
