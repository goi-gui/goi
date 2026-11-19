package ui

import (
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/tree"
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

// TestWindowThemeTransition reads the effect and its none default.
func TestWindowThemeTransition(t *testing.T) {
	_, win := reloadApp(t, `Window { themeTransition: crossfade 500ms }`)
	if got := win.Root().ThemeTransition.Get(); got.Name != "crossfade" || got.Dur != 500*time.Millisecond {
		t.Fatalf("themeTransition = %+v", got)
	}
	_, plain := reloadApp(t, `Window { }`)
	if got := plain.Root().ThemeTransition.Get(); got.Name != "" || got.Dur != 0 {
		t.Fatalf("default themeTransition = %+v, want none", got)
	}
}

// TestSeedRedefinitionCrossfades: DefineTheme can replace a scheme under the
// same name (the shopping seed swatches do), so the target values, not the name,
// decide that the theme changed.
func TestSeedRedefinitionCrossfades(t *testing.T) {
	a, win := reloadApp(t, `Window { theme: brand themeTransition: crossfade 500ms }`)
	first := m3.Light()
	first.Name = "brand"
	first.Colors.Primary = 0x112233FF
	win.DefineTheme(first)
	if got := a.displayTheme(win); got.Colors.Primary != 0x112233FF {
		t.Fatalf("first scheme = %08x", got.Colors.Primary)
	}
	second := m3.Light()
	second.Name = "brand"
	second.Colors.Primary = 0xDDEEFFFF
	win.DefineTheme(second)
	if got := a.displayTheme(win); !a.themeFading || got.Colors.Primary != 0x112233FF {
		t.Fatalf("seed change did not start from the old scheme: fading=%v primary=%08x", a.themeFading, got.Colors.Primary)
	}
	a.themeStart = time.Now().Add(-250 * time.Millisecond)
	mid := a.displayTheme(win).Colors.Primary
	if mid == 0x112233FF || mid == 0xDDEEFFFF {
		t.Fatalf("mid seed primary = %08x, want a blend", mid)
	}
	a.themeStart = time.Now().Add(-time.Second)
	if got := a.displayTheme(win).Colors.Primary; got != 0xDDEEFFFF {
		t.Fatalf("final seed primary = %08x", got)
	}
}

// TestThemeWipeTransition: wipe reports its progress and keeps the target as
// the scheme the rest of the frame resolves, while the paint pass splits the
// old and new themes around the moving edge.
func TestThemeWipeTransition(t *testing.T) {
	a, win := reloadApp(t, `Window { theme: brand themeTransition: wipe 500ms }`)
	old := m3.Light()
	old.Name, old.Colors.Primary = "brand", 0x112233FF
	win.DefineTheme(old)
	a.displayTheme(win)
	next := m3.Light()
	next.Name, next.Colors.Primary = "brand", 0xDDEEFFFF
	win.DefineTheme(next)
	if got := a.displayTheme(win); !a.themeWipe || got.Colors.Primary != next.Colors.Primary {
		t.Fatalf("wipe did not start: wipe=%v primary=%08x", a.themeWipe, got.Colors.Primary)
	}
	a.themeStart = time.Now().Add(-250 * time.Millisecond)
	if p := a.displayTheme(win); !a.themeWipe || a.themeWipeP < 0.4 || a.themeWipeP > 0.6 || p.Colors.Primary != next.Colors.Primary {
		t.Fatalf("wipe progress = %v, primary=%08x", a.themeWipeP, p.Colors.Primary)
	}
	a.themeStart = time.Now().Add(-time.Second)
	a.displayTheme(win)
	if a.themeWipe {
		t.Fatal("wipe did not finish")
	}
}

// TestThemeWipeSoftEdge: the boundary is a gradient, not a hard split. The
// background carries a ramp between the surfaces and the content at the edge
// paints with blended roles.
func TestThemeWipeSoftEdge(t *testing.T) {
	a, win := reloadApp(t, `Window {
		theme: brand
		themeTransition: wipe 500ms
		Rectangle { width: 200 height: 200 background: theme.surface }
	}`)
	old := m3.Light()
	old.Name, old.Colors.Surface, old.Colors.Primary = "brand", 0x000000FF, 0x000000FF
	win.DefineTheme(old)
	a.displayTheme(win)
	next := m3.Light()
	next.Name, next.Colors.Surface, next.Colors.Primary = "brand", 0xFFFFFFFF, 0xFFFFFFFF
	win.DefineTheme(next)
	a.displayTheme(win)
	a.themeStart = time.Now().Add(-250 * time.Millisecond)
	a.displayTheme(win)
	layout.New(layout.FakeMeasurer{}).Layout(win.tree.Root, 400, 400)

	var l paint.List
	a.buildWipeList(&l, win.tree, 400, 400)

	var ramp *tree.Gradient
	blended, blurred := false, false
	for _, op := range l.Ops {
		if op.Shape == paint.CSSGradient {
			ramp = op.Gradient
		}
		if op.Kind == paint.Fill && op.Shape != paint.CSSGradient &&
			op.Color != old.Colors.Surface && op.Color != next.Colors.Surface {
			blended = true
		}
		if op.Kind == paint.Wipe && op.Blur > 0 {
			blurred = true
		}
	}
	if ramp == nil || len(ramp.Stops) != 4 {
		t.Fatalf("background ramp = %+v, want four stops", ramp)
	}
	if !blended {
		t.Fatal("no content painted with a blended scheme")
	}
	if !blurred {
		t.Fatal("the wipe did not ask the renderer for a blur band")
	}

	// Before the edge enters, the whole window is still the old scheme.
	a.themeWipeP = 0
	l.Reset()
	a.buildWipeList(&l, win.tree, 400, 400)
	for _, op := range l.Ops {
		if op.Shape != paint.CSSGradient {
			continue
		}
		for _, stop := range op.Gradient.Stops {
			if stop.Color != old.Colors.Surface {
				t.Fatalf("background at start = %08x, want old surface %08x", stop.Color, old.Colors.Surface)
			}
		}
	}
}
