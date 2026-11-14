//go:build js

package platform

import "fmt"

// SurfaceHandles returns the browser canvas the renderer draws into. The handle
// is the number the canvas carries as data-raw-handle, which is how the renderer
// finds this element and not another: a page with several canvases hosts one app
// each.
func (p *Window) SurfaceHandles() (display, window uintptr, kind SurfaceKind, err error) {
	if p == nil || p.canvas.IsUndefined() || p.canvas.IsNull() {
		return 0, 0, 0, fmt.Errorf("platform: window is not open")
	}
	if p.handle == 0 {
		return 0, 0, 0, fmt.Errorf("platform: canvas has no handle")
	}
	return 0, p.handle, SurfaceWeb, nil
}
