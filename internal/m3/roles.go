package m3

// Role resolves a .ui color role name such as "surfaceContainerHigh". A role is
// a name rather than a literal, so a design that uses roles follows the theme
// when the scheme switches; a hex literal stays put by design.
func (c Colors) Role(name string) (uint32, bool) {
	switch name {
	case "primary":
		return c.Primary, true
	case "onPrimary":
		return c.OnPrimary, true
	case "primaryContainer":
		return c.PrimaryContainer, true
	case "onPrimaryContainer":
		return c.OnPrimaryContainer, true
	case "secondary":
		return c.Secondary, true
	case "onSecondary":
		return c.OnSecondary, true
	case "secondaryContainer":
		return c.SecondaryContainer, true
	case "onSecondaryContainer":
		return c.OnSecondaryContainer, true
	case "tertiary":
		return c.Tertiary, true
	case "onTertiary":
		return c.OnTertiary, true
	case "tertiaryContainer":
		return c.TertiaryContainer, true
	case "onTertiaryContainer":
		return c.OnTertiaryContainer, true
	case "error":
		return c.Error, true
	case "onError":
		return c.OnError, true
	case "errorContainer":
		return c.ErrorContainer, true
	case "onErrorContainer":
		return c.OnErrorContainer, true
	case "surface":
		return c.Surface, true
	case "onSurface":
		return c.OnSurface, true
	case "surfaceVariant":
		return c.SurfaceVariant, true
	case "onSurfaceVariant":
		return c.OnSurfaceVariant, true
	case "surfaceDim":
		return c.SurfaceDim, true
	case "surfaceBright":
		return c.SurfaceBright, true
	case "surfaceContainerLowest":
		return c.SurfaceContainerLowest, true
	case "surfaceContainerLow":
		return c.SurfaceContainerLow, true
	case "surfaceContainer":
		return c.SurfaceContainer, true
	case "surfaceContainerHigh":
		return c.SurfaceContainerHigh, true
	case "surfaceContainerHighest":
		return c.SurfaceContainerHighest, true
	case "outline":
		return c.Outline, true
	case "outlineVariant":
		return c.OutlineVariant, true
	case "inverseSurface":
		return c.InverseSurface, true
	case "inverseOnSurface":
		return c.InverseOnSurface, true
	case "inversePrimary":
		return c.InversePrimary, true
	case "shadow":
		return c.Shadow, true
	case "scrim":
		return c.Scrim, true
	}
	return 0, false
}

// ColorRoles lists every accepted role name. Diagnostics use it for "did you
// mean" hints.
func ColorRoles() []string {
	return []string{
		"primary", "onPrimary", "primaryContainer", "onPrimaryContainer",
		"secondary", "onSecondary", "secondaryContainer", "onSecondaryContainer",
		"tertiary", "onTertiary", "tertiaryContainer", "onTertiaryContainer",
		"error", "onError", "errorContainer", "onErrorContainer",
		"surface", "onSurface", "surfaceVariant", "onSurfaceVariant",
		"surfaceDim", "surfaceBright", "surfaceContainerLowest",
		"surfaceContainerLow", "surfaceContainer", "surfaceContainerHigh",
		"surfaceContainerHighest", "outline", "outlineVariant", "inverseSurface",
		"inverseOnSurface", "inversePrimary", "shadow", "scrim",
	}
}
