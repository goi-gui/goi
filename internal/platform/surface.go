//go:build !js

package platform

import (
	"fmt"
)

// surfaceKindFor maps the video driver SDL selected to the surface the renderer
// must create. The driver names are SDL's, so the mapping is the one thing a
// new platform has to add here.
func surfaceKindFor(driver string) (SurfaceKind, bool) {
	switch driver {
	case "wayland":
		return SurfaceWayland, true
	case "x11":
		return SurfaceX11, true
	case "windows":
		return SurfaceWin32, true
	case "cocoa":
		return SurfaceMetal, true
	}
	return 0, false
}

// SurfaceHandles returns the native handle pair WebGPU needs for this window:
// the Wayland display and surface, the X11 Display and Window, the Win32
// HINSTANCE and HWND, or a Metal layer (with a nil display). Which one it is
// depends on the video driver SDL selected, not on a build tag.
func (p *Window) SurfaceHandles() (display, window uintptr, kind SurfaceKind, err error) {
	if p.win == nil {
		return 0, 0, 0, fmt.Errorf("platform: window is not open")
	}
	driver := sdlVideoDriver()
	kind, ok := surfaceKindFor(driver)
	if !ok {
		return 0, 0, 0, fmt.Errorf("platform: no WebGPU surface for the %q video driver", driver)
	}
	switch kind {
	case SurfaceWayland:
		disp, surf := sdlWaylandHandles(p.win)
		if disp == nil || surf == nil {
			return 0, 0, 0, fmt.Errorf("platform: SDL returned nil wayland handles")
		}
		return uintptr(disp), uintptr(surf), kind, nil
	case SurfaceX11:
		disp, id := sdlX11Handles(p.win)
		if disp == nil || id == 0 {
			return 0, 0, 0, fmt.Errorf("platform: SDL returned nil x11 handles")
		}
		return uintptr(disp), id, kind, nil
	case SurfaceWin32:
		inst, hwnd := sdlWin32Handles(p.win)
		if hwnd == nil {
			return 0, 0, 0, fmt.Errorf("platform: SDL returned a nil win32 window")
		}
		return uintptr(inst), uintptr(hwnd), kind, nil
	case SurfaceMetal:
		layer := sdlMetalLayer(p.win)
		if layer == nil {
			return 0, 0, 0, fmt.Errorf("platform: SDL returned a nil metal layer")
		}
		return 0, uintptr(layer), kind, nil
	}
	return 0, 0, 0, fmt.Errorf("platform: no WebGPU surface for %v", kind)
}
