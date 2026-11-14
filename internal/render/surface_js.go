//go:build js

package render

import "github.com/gogpu/wgpu"

// createSurface takes the canvas the window named. A browser surface is found
// by the number the canvas carries as data-raw-handle, which is why the window
// handle is the canvas id and not a pointer.
func createSurface(inst *wgpu.Instance, _, window uintptr, _ TargetKind) (*wgpu.Surface, error) {
	return inst.CreateSurfaceUnsafe(wgpu.SurfaceTargetFromWebCanvasID(window))
}
