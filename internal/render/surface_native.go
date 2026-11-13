//go:build !js

package render

import "github.com/gogpu/wgpu"

// createSurface makes the surface from a native window handle pair.
func createSurface(inst *wgpu.Instance, display, window uintptr, kind TargetKind) (*wgpu.Surface, error) {
	target := wgpu.SurfaceTargetFromWaylandSurface(display, window)
	switch kind {
	case TargetX11:
		target = wgpu.SurfaceTargetFromXlibWindow(display, window)
	case TargetWin32:
		target = wgpu.SurfaceTargetFromWindowsHWND(display, window)
	case TargetMetal:
		target = wgpu.SurfaceTargetFromMetalLayer(window)
	}
	return inst.CreateSurfaceUnsafe(target)
}
