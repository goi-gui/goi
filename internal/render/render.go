// Package render draws a paint.List. Only this package (and platform) may
// import a graphics API; everything above it works with plain data.
package render

import "github.com/ksckaan1/goi/internal/paint"

// Renderer draws display lists into a window surface.
type Renderer interface {
	// Resize reconfigures the surface for a new framebuffer size in device
	// pixels, with the given content scale (logical to device ratio).
	Resize(width, height int, scale float32) error
	// Render draws the list and presents the frame. width and height are the
	// framebuffer size in device pixels.
	Render(list *paint.List, width, height int) error
	// Release frees every GPU resource.
	Release()
}

// FontLibrary returns the renderer's text library when it has one, so callers
// can register extra fonts and use the same faces for measuring.
func FontLibrary(r Renderer) (any, bool) {
	type hasLib interface{ TextLibrary() any }
	if h, ok := r.(hasLib); ok {
		return h.TextLibrary(), true
	}
	return nil, false
}
