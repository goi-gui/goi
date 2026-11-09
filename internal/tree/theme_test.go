package tree

import (
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/syntax"
)

// TestRoleColorsFollowTheScheme is the point of roles: the same .goi value paints
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

// TestExpressionColorRole covers a role picked by an expression: `x ? theme.X
// : theme.Y` must resolve to the scheme's colours, keep following a theme
// change, and an empty arm must clear the colour rather than leave the other
// arm's role behind.
func TestExpressionColorRole(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { i float64 }
Window {
	Column {
		Rectangle { id: first  background: bridge.i == 0 ? theme.primary : "" }
		Rectangle { id: second background: bridge.i == 1 ? theme.primary : "" }
		Rectangle { id: picked background: app.dark ? theme.primary : customColor.brand }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetData("i", 1)
	light := m3.Light().Colors
	tr.ApplyExpressions()
	lookup := func(name string) (uint32, bool) {
		if name == "custom:brand" {
			return 0x123456FF, true
		}
		return light.Role(strings.TrimPrefix(name, "theme:"))
	}
	tr.ResolveColors(lookup)

	first := tr.ids["first"].Base()
	if got := first.Background(); got != 0 {
		t.Fatalf("the false arm did not clear the colour: %08x", got)
	}
	second := tr.ids["second"].Base()
	if got := second.Background(); got != light.Primary {
		t.Fatalf("second = %08x, want primary %08x", got, light.Primary)
	}

	picked := tr.ids["picked"].Base()
	if got := picked.Background(); got != 0x123456FF {
		t.Fatalf("picked = %08x, want the custom colour", got)
	}
	tr.SetDark(true)
	tr.ApplyExpressions()
	tr.ResolveColors(lookup)
	if got := picked.Background(); got != light.Primary {
		t.Fatalf("picked after dark = %08x, want primary %08x", got, light.Primary)
	}

	// The cleared arm carries no role, so a later theme cannot put a colour
	// back on it.
	if got := first.colorRoleFor("background"); got != "" {
		t.Fatalf("the cleared arm kept role %q", got)
	}
}

// TestExpressionGradientAndKeyword covers the other two colour forms an
// expression can produce: a quoted gradient must paint a gradient rather than be
// mistaken for a role name, and a CSS keyword must paint its colour.
func TestExpressionGradientAndKeyword(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge { on bool }
Window {
	Column {
		Rectangle { id: grad background: bridge.on ? "linear-gradient(90deg, #ff0000, #0000ff)" : "" }
		Rectangle { id: kw   background: bridge.on ? "tomato" : "" }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetData("on", true)
	tr.ApplyExpressions()

	grad := tr.ids["grad"].Base()
	if g := grad.Gradient("background"); g == nil {
		t.Fatalf("gradient expression produced no gradient; role %q", grad.colorRoleFor("background"))
	}
	if got := grad.colorRoleFor("background"); got != "" {
		t.Fatalf("gradient was stored as role %q", got)
	}
	if got := tr.ids["kw"].Base().Background(); got != 0xFF6347FF {
		t.Fatalf("keyword = %08x, want tomato", got)
	}
	tr.SetData("on", false)
	tr.ApplyExpressions()
	if g := tr.ids["grad"].Base().Gradient("background"); g != nil {
		t.Fatal("the cleared arm kept its gradient")
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

// TestGoSetColorReplacesRole is the same rule for Go: `Color.Set` must win over
// the design's role, or the frame loop would put the scheme's colour back on the
// next tick.
func TestGoSetColorReplacesRole(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window { Text { id: label text: "x" color: primary } }`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	label := tr.ids["label"].(*Text)
	tr.ResolveColors(m3.Light().Colors.Role)
	label.Color.Set(0x123456FF)
	tr.ResolveColors(m3.Light().Colors.Role)
	if got := label.Color.Get(); got != 0x123456FF {
		t.Fatalf("Go colour = %08x, the role put its own back", got)
	}
	if got := label.Base().colorRoleFor("color"); got != "" {
		t.Fatalf("role %q survived a Go write", got)
	}
}
