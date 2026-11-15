package paint

import "testing"

// TestFABMenuTakesTheFABColors keeps the menu's variant in step with the FAB it
// opens from: primary, secondary and tertiary pick the same container pairs.
func TestFABMenuTakesTheFABColors(t *testing.T) {
	l := list(t, `Window { FABMenu { icon: add } }`, 400, 300)
	want := []uint32{l.Theme.Colors.PrimaryContainer,
		l.Theme.Colors.SecondaryContainer, l.Theme.Colors.TertiaryContainer}
	for i, variant := range []string{"primary", "secondary", "tertiary"} {
		got := uint32(0)
		for _, op := range list(t, `Window { FABMenu { icon: add variant: `+variant+` } }`, 400, 300).Ops {
			if op.Kind == Fill && op.Rect[2] == 56 && op.Rect[3] == 56 {
				got = op.Color
				break
			}
		}
		if got != want[i] {
			t.Errorf("%s FAB container = %08x, want %08x", variant, got, want[i])
		}
	}
}
