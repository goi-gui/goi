//go:build linux && !js

package platform

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

// TestPortalThemeMapping keeps the freedesktop value (1 dark, 2 light) apart
// from the platform's own Theme order (1 light, 2 dark).
func TestPortalThemeMapping(t *testing.T) {
	old := portalThemeValue.Load()
	t.Cleanup(func() { portalThemeValue.Store(old) })
	for _, tc := range []struct {
		value uint32
		want  Theme
	}{
		{0, ThemeUnknown},
		{1, ThemeDark},
		{2, ThemeLight},
	} {
		setPortalTheme(tc.value)
		if got := portalTheme(); got != tc.want {
			t.Errorf("portal %d = %v, want %v", tc.value, got, tc.want)
		}
	}
	for _, v := range []any{uint32(2), int32(1), dbus.MakeVariant(uint32(1))} {
		if _, ok := portalUint(v); !ok {
			t.Errorf("portalUint(%T) rejected a value", v)
		}
	}
	if _, ok := portalUint("dark"); ok {
		t.Error("portalUint accepted a string")
	}
}
