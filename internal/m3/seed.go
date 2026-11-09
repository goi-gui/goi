package m3

import "math"

// FromSeed builds a Material 3 scheme from one seed colour, in the light or the
// dark mood. The seed sets the hue: primary keeps it, secondary is the same hue
// at a lower chroma, tertiary sits 60° around the wheel and the neutrals keep a
// trace of it.
//
// The conversion is CIELCh rather than Material's HCT, so the result is close to
// a Material scheme without a CAM16 implementation: tones are L*, and a chroma
// that would leave sRGB is reduced until it fits.
func FromSeed(seed uint32, dark bool) *Theme {
	r := float64(seed>>24) / 255
	g := float64(seed>>16&0xFF) / 255
	b := float64(seed>>8&0xFF) / 255
	_, a, bb := rgbToLab(r, g, b)
	hue := math.Atan2(bb, a) * 180 / math.Pi
	// A vivid seed gives a vivid scheme, a muted one a muted scheme; the clamps
	// keep a nearly grey seed from producing a saturated hue.
	chroma := math.Hypot(a, bb)
	p := palette{hue, clampf(chroma*0.6, 8, 36)}
	s := palette{hue, clampf(chroma*0.25, 4, 16)}
	t := palette{hue + 60, clampf(chroma*0.4, 6, 24)}
	n := palette{hue, clampf(chroma*0.06, 1.5, 4)}
	nv := palette{hue, clampf(chroma*0.12, 3, 8)}

	th := &Theme{Name: "lightFromSeed", Type: typeScale}
	if dark {
		th.Name, th.Dark = "darkFromSeed", true
	}
	c := &th.Colors
	c.Shadow, c.Scrim = rgba(0x000000), rgba(0x000000)
	if dark {
		c.Primary, c.OnPrimary = p.tone(80), p.tone(20)
		c.PrimaryContainer, c.OnPrimaryContainer = p.tone(30), p.tone(90)
		c.Secondary, c.OnSecondary = s.tone(80), s.tone(20)
		c.SecondaryContainer, c.OnSecondaryContainer = s.tone(30), s.tone(90)
		c.Tertiary, c.OnTertiary = t.tone(80), t.tone(20)
		c.TertiaryContainer, c.OnTertiaryContainer = t.tone(30), t.tone(90)
		c.Error, c.OnError = rgba(0xF2B8B5), rgba(0x601410)
		c.ErrorContainer, c.OnErrorContainer = rgba(0x8C1D18), rgba(0xF9DEDC)
		c.Surface, c.OnSurface = n.tone(6), n.tone(90)
		c.SurfaceVariant, c.OnSurfaceVariant = nv.tone(30), nv.tone(80)
		c.SurfaceDim, c.SurfaceBright = n.tone(6), n.tone(24)
		c.SurfaceContainerLowest, c.SurfaceContainerLow = n.tone(4), n.tone(10)
		c.SurfaceContainer, c.SurfaceContainerHigh = n.tone(12), n.tone(17)
		c.SurfaceContainerHighest = n.tone(22)
		c.Outline, c.OutlineVariant = nv.tone(60), nv.tone(30)
		c.InverseSurface, c.InverseOnSurface = n.tone(90), n.tone(20)
		c.InversePrimary = p.tone(40)
		return th
	}
	c.Primary, c.OnPrimary = p.tone(40), p.tone(100)
	c.PrimaryContainer, c.OnPrimaryContainer = p.tone(90), p.tone(10)
	c.Secondary, c.OnSecondary = s.tone(40), s.tone(100)
	c.SecondaryContainer, c.OnSecondaryContainer = s.tone(90), s.tone(10)
	c.Tertiary, c.OnTertiary = t.tone(40), t.tone(100)
	c.TertiaryContainer, c.OnTertiaryContainer = t.tone(90), t.tone(10)
	c.Error, c.OnError = rgba(0xB3261E), rgba(0xFFFFFF)
	c.ErrorContainer, c.OnErrorContainer = rgba(0xF9DEDC), rgba(0x410E0B)
	c.Surface, c.OnSurface = n.tone(98), n.tone(10)
	c.SurfaceVariant, c.OnSurfaceVariant = nv.tone(90), nv.tone(30)
	c.SurfaceDim, c.SurfaceBright = n.tone(87), n.tone(98)
	c.SurfaceContainerLowest, c.SurfaceContainerLow = n.tone(100), n.tone(96)
	c.SurfaceContainer, c.SurfaceContainerHigh = n.tone(94), n.tone(92)
	c.SurfaceContainerHighest = n.tone(90)
	c.Outline, c.OutlineVariant = nv.tone(50), nv.tone(80)
	c.InverseSurface, c.InverseOnSurface = n.tone(20), n.tone(95)
	c.InversePrimary = p.tone(80)
	return th
}

// palette is one tonal palette: a hue and the most chroma any tone may use.
type palette struct{ h, c float64 }

// tone returns the palette's colour at a CIELAB lightness, lowering chroma
// until the result fits sRGB.
func (p palette) tone(lightness float64) uint32 {
	c := p.c
	if !inGamut(lightness, c, p.h) {
		lo, hi := 0.0, c
		for i := 0; i < 20; i++ {
			mid := (lo + hi) / 2
			if inGamut(lightness, mid, p.h) {
				lo = mid
			} else {
				hi = mid
			}
		}
		c = lo
	}
	r, g, b := lchToRGB(lightness, c, p.h)
	return packRGB(r, g, b)
}

func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func packRGB(r, g, b float64) uint32 {
	to8 := func(v float64) uint32 {
		v = clampf(v, 0, 1)
		return uint32(math.Round(v * 255))
	}
	return to8(r)<<24 | to8(g)<<16 | to8(b)<<8 | 0xFF
}

// sRGB and CIELAB (D65) conversion. Lab is the tonal axis: tone *is* L*.

func srgbToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSrgb(v float64) float64 {
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

func labF(t float64) float64 {
	const kappa = 24389.0 / 27.0
	if t > 216.0/24389.0 {
		return math.Cbrt(t)
	}
	return (kappa*t + 16) / 116
}

func labFInv(t float64) float64 {
	const kappa = 24389.0 / 27.0
	if t*t*t > 216.0/24389.0 {
		return t * t * t
	}
	return (116*t - 16) / kappa
}

func rgbToLab(r, g, b float64) (lightness, a, bb float64) {
	r, g, b = srgbToLinear(r), srgbToLinear(g), srgbToLinear(b)
	x := 0.4124564*r + 0.3575761*g + 0.1804375*b
	y := 0.2126729*r + 0.7151522*g + 0.0721750*b
	z := 0.0193339*r + 0.1191920*g + 0.9503041*b
	fx, fy, fz := labF(x/0.95047), labF(y), labF(z/1.08883)
	lightness = 116*fy - 16
	a = 500 * (fx - fy)
	bb = 200 * (fy - fz)
	return
}

func lchToRGB(lightness, chroma, hue float64) (r, g, b float64) {
	rad := hue * math.Pi / 180
	a := chroma * math.Cos(rad)
	bb := chroma * math.Sin(rad)
	fy := (lightness + 16) / 116
	x := 0.95047 * labFInv(fy+a/500)
	y := labFInv(fy)
	z := 1.08883 * labFInv(fy-bb/200)
	r = linearToSrgb(3.2404542*x - 1.5371385*y - 0.4985314*z)
	g = linearToSrgb(-0.9692660*x + 1.8760108*y + 0.0415560*z)
	b = linearToSrgb(0.0556434*x - 0.2040259*y + 1.0572252*z)
	return
}

func inGamut(lightness, chroma, hue float64) bool {
	const eps = 1e-4
	r, g, b := lchToRGB(lightness, chroma, hue)
	return r >= -eps && r <= 1+eps && g >= -eps && g <= 1+eps && b >= -eps && b <= 1+eps
}
