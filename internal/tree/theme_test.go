package tree

import (
	"testing"

	"github.com/ksckaan1/goui/internal/m3"
	"github.com/ksckaan1/goui/internal/syntax"
)

// TestRoleColorsFollowTheScheme is the point of roles: the same .ui value paints
// light in light mode and dark in dark mode, without touching the design.
func TestRoleColorsFollowTheScheme(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Rectangle { id: box background: surfaceContainerHigh borderColor: onSurface }
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	if got := box.Background(); got != 0 {
		t.Fatalf("unresolved background = %08x, want 0", got)
	}
	light := m3.Light().Colors
	tr.ResolveColors(light.Role)
	if got := box.Background(); got != light.SurfaceContainerHigh {
		t.Fatalf("light background = %08x, want %08x", got, light.SurfaceContainerHigh)
	}
	dark := m3.Dark().Colors
	tr.ResolveColors(dark.Role)
	if got := box.Background(); got != dark.SurfaceContainerHigh {
		t.Fatalf("dark background = %08x, want %08x", got, dark.SurfaceContainerHigh)
	}
	if got := box.BorderColor(); got != dark.OnSurface {
		t.Fatalf("dark border = %08x, want %08x", got, dark.OnSurface)
	}
	if _, dirty := box.Dirty(); !dirty {
		t.Fatal("a role change did not mark the node for repaint")
	}
}

// TestLiteralColorReplacesRole guards hot reload: a literal written over a role
// must stop following the theme.
func TestLiteralColorReplacesRole(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window { Rectangle { id: box background: primary } }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	box := tr.ids["box"].Base()
	box.SetPropLiteral("background", &syntax.Color{RGBA: 0xFF0000FF})
	tr.ResolveColors(m3.Dark().Colors.Role)
	if got := box.Background(); got != 0xFF0000FF {
		t.Fatalf("literal was overwritten by a role: %08x", got)
	}
}
