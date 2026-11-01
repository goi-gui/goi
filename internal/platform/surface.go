package platform

import (
	"fmt"
)

// SurfaceHandles returns the native handle pair WebGPU needs for this window:
// the Wayland display and surface, or the X11 Display and Window. Which pair it
// is depends on the video driver SDL selected, not on a build tag.
func (p *Window) SurfaceHandles() (display, window uintptr, kind SurfaceKind, err error) {
	if p.win == nil {
		return 0, 0, 0, fmt.Errorf("platform: window is not open")
	}
	switch d := sdlVideoDriver(); d {
	case "wayland":
		disp, surf := sdlWaylandHandles(p.win)
		if disp == nil || surf == nil {
			return 0, 0, 0, fmt.Errorf("platform: SDL returned nil wayland handles")
		}
		return uintptr(disp), uintptr(surf), SurfaceWayland, nil
	case "x11":
		disp, id := sdlX11Handles(p.win)
		if disp == nil || id == 0 {
			return 0, 0, 0, fmt.Errorf("platform: SDL returned nil x11 handles")
		}
		return uintptr(disp), id, SurfaceX11, nil
	default:
		return 0, 0, 0, fmt.Errorf("platform: no WebGPU surface for the %q video driver", d)
	}
}
