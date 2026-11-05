package ui

import (
	"testing"

	"github.com/ksckaan1/goi/internal/m3"
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

// TestReservedCustomColorIsRejected keeps a scheme role owned by the scheme: a
// custom colour may not shadow `primary`, so a design that names it keeps
// following the theme.
func TestReservedCustomColorIsRejected(t *testing.T) {
	a, w := reloadApp(t, `Window { Rectangle { id: box background: primary } }`)
	box := MustByID[*Rectangle](w, "box")

	w.SetCustomColor("primary", 0xFF0000FF)
	a.resolveColors()

	if got := box.Background(); got != m3.Light().Colors.Primary {
		t.Fatalf("background = %08x, a reserved name shadowed the scheme", got)
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
