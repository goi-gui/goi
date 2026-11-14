package csscolor

import "testing"

// TestGradientColourVariables keeps a role inside a gradient: a stop names its
// namespace and stays a name until the caller resolves it, while a literal stop
// beside it is final.
func TestGradientColourVariables(t *testing.T) {
	g, ok := ParseGradient("linear-gradient(90deg, theme.primary, customColor.brand 60%, #ffffff)")
	if !ok {
		t.Fatal("not parsed")
	}
	if len(g.Stops) != 3 {
		t.Fatalf("stops = %d, want 3", len(g.Stops))
	}
	if g.Stops[0].Role != "theme:primary" || g.Stops[1].Role != "custom:brand" {
		t.Fatalf("roles = %q %q", g.Stops[0].Role, g.Stops[1].Role)
	}
	if g.Stops[1].At != 0.6 {
		t.Fatalf("position = %v, want 0.6", g.Stops[1].At)
	}
	if g.Stops[2].Role != "" || g.Stops[2].Color != 0xFFFFFFFF {
		t.Fatalf("literal stop = %+v", g.Stops[2])
	}
	if !g.HasRoles() {
		t.Fatal("HasRoles said no")
	}
	if g, ok := ParseGradient("linear-gradient(theme.primary, red)"); !ok || g.Stops[1].Color != 0xFF0000FF {
		t.Fatal("a role and a CSS name should be two stops")
	}
	if _, ok := ParseGradient("linear-gradient(theme.primary)"); ok {
		t.Fatal("a gradient needs two stops")
	}
}
