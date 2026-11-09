package m3

import (
	"math"
	"testing"
)

// TestFromSeedNames pins the naming rule the UI relies on: a design selects the
// scheme as `lightFromSeed` or `darkFromSeed`.
func TestFromSeedNames(t *testing.T) {
	light := FromSeed(0x6750A4FF, false)
	dark := FromSeed(0x6750A4FF, true)
	if light.Name != "lightFromSeed" || light.Dark {
		t.Fatalf("light = %q dark=%v", light.Name, light.Dark)
	}
	if dark.Name != "darkFromSeed" || !dark.Dark {
		t.Fatalf("dark = %q dark=%v", dark.Name, dark.Dark)
	}
	if light.Type != dark.Type {
		t.Fatal("the type scale varies between moods")
	}
}

func TestFromSeedIsDeterministic(t *testing.T) {
	a := FromSeed(0x2E7D32FF, false)
	b := FromSeed(0x2E7D32FF, false)
	if a.Colors != b.Colors {
		t.Fatal("the same seed produced two schemes")
	}
	if a.Colors == FromSeed(0x2E7D32FF, true).Colors {
		t.Fatal("light and dark share every colour")
	}
}

// TestFromSeedHueFollowsTheSeed is the point of the feature: a green seed must
// not produce a purple scheme.
func TestFromSeedHueFollowsTheSeed(t *testing.T) {
	c := FromSeed(0x2E7D32FF, false).Colors.Primary
	r, g, b := int(c>>24), int(c>>16&0xFF), int(c>>8&0xFF)
	if g <= r || g <= b {
		t.Fatalf("green seed gave primary #%08x", c)
	}
}

// TestFromSeedRolesAreOpaqueAndReadable checks that every role is a real colour
// and that the text pairs stay readable, which gamut mapping must not break.
func TestFromSeedRolesAreOpaqueAndReadable(t *testing.T) {
	seeds := []uint32{0x6750A4FF, 0x2E7D32FF, 0xFF0000FF, 0x808080FF, 0xFFFFFFFF}
	for _, seed := range seeds {
		for _, dark := range []bool{false, true} {
			th := FromSeed(seed, dark)
			for _, name := range ColorRoles() {
				c, ok := th.Colors.Role(name)
				if !ok {
					t.Fatalf("%s missing", name)
				}
				if c&0xFF != 0xFF {
					t.Fatalf("%s = #%08x is not opaque", name, c)
				}
			}
			for _, pair := range [][2]uint32{
				{th.Colors.Primary, th.Colors.OnPrimary},
				{th.Colors.PrimaryContainer, th.Colors.OnPrimaryContainer},
				{th.Colors.Surface, th.Colors.OnSurface},
			} {
				if got := contrast(pair[0], pair[1]); got < 4.5 {
					t.Fatalf("seed #%08x dark=%v: contrast %.2f", seed, dark, got)
				}
			}
		}
	}
}

// contrast is the WCAG ratio of two opaque colours.
func contrast(a, b uint32) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(c uint32) float64 {
	ch := func(v uint32) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c>>24) + 0.7152*ch(c>>16&0xFF) + 0.0722*ch(c>>8&0xFF)
}
