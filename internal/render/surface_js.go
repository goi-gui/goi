//go:build js

package render

import "github.com/gogpu/wgpu"

// createSurface takes the page's canvas. A browser has no native handles: the
// canvas is found by the script that hosts the app, so the id is unused and the
// first canvas element is the surface.
func createSurface(inst *wgpu.Instance, _, _ uintptr, _ TargetKind) (*wgpu.Surface, error) {
	return inst.CreateSurfaceUnsafe(wgpu.SurfaceTargetFromWebCanvasID(0))
}
