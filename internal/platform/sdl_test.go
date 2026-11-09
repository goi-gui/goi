package platform

import "testing"

// TestWantWaylandDriver pins the driver choice: a Wayland session gets SDL's
// native driver, and an explicit SDL_VIDEODRIVER still wins.
func TestWantWaylandDriver(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"wayland session", map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, true},
		{"explicit driver", map[string]string{"WAYLAND_DISPLAY": "wayland-1", "SDL_VIDEODRIVER": "x11"}, false},
		{"x11 session", map[string]string{"DISPLAY": ":0"}, false},
		{"empty session", map[string]string{}, false},
	}
	for _, c := range cases {
		got := wantWaylandDriver(func(k string) string { return c.env[k] })
		if got != c.want {
			t.Errorf("%s: wantWaylandDriver = %v, want %v", c.name, got, c.want)
		}
	}
}
