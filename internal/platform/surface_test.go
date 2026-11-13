//go:build !js

package platform

import "testing"

// TestSurfaceKindFor pins the driver-name mapping: SDL names the driver, and
// each name selects the native handle pair the renderer asks for.
func TestSurfaceKindFor(t *testing.T) {
	cases := []struct {
		driver string
		kind   SurfaceKind
		ok     bool
	}{
		{"wayland", SurfaceWayland, true},
		{"x11", SurfaceX11, true},
		{"windows", SurfaceWin32, true},
		{"cocoa", SurfaceMetal, true},
		{"dummy", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		kind, ok := surfaceKindFor(c.driver)
		if kind != c.kind || ok != c.ok {
			t.Errorf("surfaceKindFor(%q) = %v, %v; want %v, %v", c.driver, kind, ok, c.kind, c.ok)
		}
	}
}
