package ui

import (
	"testing"

	"github.com/goi-gui/goi/internal/m3"
)

// TestCustomColorResolves checks the design side of SetCustomColor: a name that
// is not a scheme role resolves to the value the app defined.
func TestCustomColorResolves(t *testing.T) {
	a, w := reloadApp(t, `Window { Rectangle { id: box background: myBrand } }`)
	box := MustByID[*Rectangle](w, "box")

	w.SetCustomColor("myBrand", 0x411F89FF)
	if !a.resolveColors() {
		t.Fatal("the custom colour did not change anything")
	}
	if got := box.Background(); got != 0x411F89FF {
		t.Fatalf("background = %08x, want the custom colour", got)
	}

	// Setting it again updates the value: that is what reactive recolouring is.
	w.SetCustomColor("myBrand", 0x112233FF)
	if !a.resolveColors() {
		t.Fatal("an updated custom colour did not repaint")
	}
	if got := box.Background(); got != 0x112233FF {
		t.Fatalf("background = %08x, want the updated colour", got)
	}
}

// TestCustomColorsMap covers the plural form.
func TestCustomColorsMap(t *testing.T) {
	a, w := reloadApp(t, `Window { Column {
		Rectangle { id: a background: myBrand }
		Rectangle { id: b background: otherColor }
	} }`)
	w.SetCustomColors(map[string]uint32{
		"myBrand":    0x411F89FF,
		"otherColor": 0x2C165CFF,
	})
	a.resolveColors()

	if got := MustByID[*Rectangle](w, "a").Background(); got != 0x411F89FF {
		t.Fatalf("a = %08x", got)
	}
	if got := MustByID[*Rectangle](w, "b").Background(); got != 0x2C165CFF {
		t.Fatalf("b = %08x", got)
	}
}

// TestCustomColorMayReuseARoleName is why nothing is reserved: a design says
// `theme.primary` for the scheme and `customColor.primary` for the app's own, so
// the two names live in different namespaces and never meet.
func TestCustomColorMayReuseARoleName(t *testing.T) {
	a, w := reloadApp(t, `Window {
		Rectangle { id: themed background: theme.primary }
		Rectangle { id: own background: customColor.primary }
	}`)
	themed := MustByID[*Rectangle](w, "themed")
	own := MustByID[*Rectangle](w, "own")

	w.SetCustomColor("primary", 0xFF0000FF)
	a.resolveColors()

	if got := themed.Background(); got != m3.Light().Colors.Primary {
		t.Fatalf("theme.primary = %08x, want the scheme's role", got)
	}
	if got := own.Background(); got != 0xFF0000FF {
		t.Fatalf("customColor.primary = %08x, want the app's colour", got)
	}
}

// TestMissingCustomColorFallsBack keeps a typo visible: the element still draws
// a theme colour (onSurface) instead of nothing.
func TestMissingCustomColorFallsBack(t *testing.T) {
	a, w := reloadApp(t, `Window { Rectangle { id: box background: notDefined } }`)
	box := MustByID[*Rectangle](w, "box")

	a.resolveColors()
	if got := box.Background(); got != m3.Light().Colors.OnSurface {
		t.Fatalf("background = %08x, want the onSurface fallback", got)
	}
}

// TestColorNameSyntax guards the identifier rule: a name the lexer cannot read
// as a bare word would never match a design value.
func TestColorNameSyntax(t *testing.T) {
	for _, name := range []string{"myBrand", "brand_2", "surfaceAlt"} {
		if !validColorName(name) {
			t.Errorf("%q rejected", name)
		}
	}
	for _, name := range []string{"", "2brand", "my-brand", "my brand", "$brand"} {
		if validColorName(name) {
			t.Errorf("%q accepted", name)
		}
	}
}
