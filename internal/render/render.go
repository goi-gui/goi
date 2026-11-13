// Package render draws a paint.List. Only this package (and platform) may
// import a graphics API; everything above it works with plain data.
package render

import (
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/text"
)

// Renderer draws display lists into a window surface.
type Renderer interface {
	// Resize reconfigures the surface for a new framebuffer size in device
	// pixels, with the given content scale (logical to device ratio).
	Resize(width, height int, scale float32) error
	// SetVSync ties presentation to the display refresh, which is the default.
	// With it off a frame is presented as soon as it is ready, so the app can
	// draw faster than the panel.
	SetVSync(on bool) error
	// SetSamples chooses the multisample count (1, 2, 4 or 8) the surface is
	// drawn with, falling back to a count the device accepts. Samples reports
	// what it settled on.
	SetSamples(samples int) error
	Samples() int
	// Render draws the list and presents the frame. width and height are the
	// framebuffer size in device pixels.
	Render(list *paint.List, width, height int) error
	// Release frees every GPU resource.
	Release()
}

// FontLibrary returns the renderer's text library, so callers can register extra
// fonts and the same faces measure and draw them.
func FontLibrary(r Renderer) (*text.Library, bool) {
	if g, ok := r.(*GPU); ok {
		return g.Text(), true
	}
	return nil, false
}
