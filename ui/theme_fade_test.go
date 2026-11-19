package ui

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/m3"
)

// TestBlendThemeMixesEveryRole keeps a theme fade between its two schemes and
// follows the target's mood and name.
func TestBlendThemeMixesEveryRole(t *testing.T) {
	light, dark := m3.Light(), m3.Dark()
	mid := blendTheme(light, dark, 0.5)
	if mid.Name != dark.Name || !mid.Dark {
		t.Fatalf("mid theme = %q dark=%v, want the target identity", mid.Name, mid.Dark)
	}
	if mid.Colors.Primary == light.Colors.Primary || mid.Colors.Primary == dark.Colors.Primary {
		t.Fatalf("primary did not mix: %08x", mid.Colors.Primary)
	}
	if mid.Colors.Surface == light.Colors.Surface || mid.Colors.Surface == dark.Colors.Surface {
		t.Fatalf("surface did not mix: %08x", mid.Colors.Surface)
	}
	if blendTheme(light, dark, 0) != light || blendTheme(light, dark, 1) != dark {
		t.Fatal("the ends are not the two schemes")
	}
}

// TestWindowCrossfadeTheme reads the duration property and its zero default.
func TestWindowCrossfadeTheme(t *testing.T) {
	_, win := reloadApp(t, `Window { crossfadeTheme: 500ms }`)
	if got := win.Root().CrossfadeTheme.Get(); got != 500*time.Millisecond {
		t.Fatalf("crossfadeTheme = %v", got)
	}
	_, plain := reloadApp(t, `Window { }`)
	if got := plain.Root().CrossfadeTheme.Get(); got != 0 {
		t.Fatalf("default crossfadeTheme = %v, want 0", got)
	}
}
