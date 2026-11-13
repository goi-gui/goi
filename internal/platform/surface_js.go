//go:build js

package platform

import "fmt"

// SurfaceHandles returns the browser canvas the renderer draws into. A canvas
// is addressed by the page, not by a raw handle, so the pair is empty and the
// kind says so.
func (p *Window) SurfaceHandles() (display, window uintptr, kind SurfaceKind, err error) {
	if p == nil || p.canvas.IsUndefined() || p.canvas.IsNull() {
		return 0, 0, 0, fmt.Errorf("platform: window is not open")
	}
	return 0, 0, SurfaceWeb, nil
}
