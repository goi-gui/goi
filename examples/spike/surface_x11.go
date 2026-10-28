//go:build linux && x11

package main

import (
	"fmt"
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
	"github.com/gogpu/wgpu"
)

// createSurface builds a WebGPU surface from GLFW's X11 handles.
func (a *app) createSurface() (*wgpu.Surface, error) {
	display := glfw.GetX11Display()
	window := a.win.GetX11Window()
	if display == nil || window == 0 {
		return nil, fmt.Errorf("glfw returned nil x11 handles")
	}
	return a.instance.CreateSurfaceUnsafe(wgpu.SurfaceTargetFromXlibWindow(
		uintptr(unsafe.Pointer(display)),
		uintptr(window),
	))
}
