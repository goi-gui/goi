//go:build linux && !x11

package main

import (
	"fmt"
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
	"github.com/gogpu/wgpu"
)

// createSurface builds a WebGPU surface from GLFW's Wayland handles.
func (a *app) createSurface() (*wgpu.Surface, error) {
	display := glfw.GetWaylandDisplay()
	surface := a.win.GetWaylandWindow()
	if display == nil || surface == nil {
		return nil, fmt.Errorf("glfw returned nil wayland handles")
	}
	return a.instance.CreateSurfaceUnsafe(wgpu.SurfaceTargetFromWaylandSurface(
		uintptr(unsafe.Pointer(display)),
		uintptr(unsafe.Pointer(surface)),
	))
}
