// Package m3 holds the Material 3 design tokens: color roles, the type scale,
// the shape scale and elevation. Every component reads its visuals from here,
// so the whole UI follows one theme.
package m3

// Theme is a full Material 3 color scheme plus the scales that do not vary
// between light and dark.
type Theme struct {
	Name   string
	Dark   bool
	Colors Colors
	Type   TypeScale
}

// Colors are the Material 3 color roles, as 0xRRGGBBAA.
type Colors struct {
	Primary, OnPrimary                       uint32
	PrimaryContainer, OnPrimaryContainer     uint32
	Secondary, OnSecondary                   uint32
	SecondaryContainer, OnSecondaryContainer uint32
	Tertiary, OnTertiary                     uint32
	TertiaryContainer, OnTertiaryContainer   uint32
	Error, OnError                           uint32
	ErrorContainer, OnErrorContainer         uint32

	Surface, OnSurface               uint32
	SurfaceVariant, OnSurfaceVariant uint32
	SurfaceDim, SurfaceBright        uint32
	SurfaceContainerLowest           uint32
	SurfaceContainerLow              uint32
	SurfaceContainer                 uint32
	SurfaceContainerHigh             uint32
	SurfaceContainerHighest          uint32

	Outline, OutlineVariant          uint32
	InverseSurface, InverseOnSurface uint32
	InversePrimary                   uint32
	Shadow, Scrim                    uint32
}

// rgba builds a color from 0xRRGGBB.
func rgba(rgb uint32) uint32 { return rgb<<8 | 0xFF }

// Light is the Material 3 baseline light scheme.
func Light() *Theme {
	return &Theme{
		Name: "baseline-light",
		Colors: Colors{
			Primary: rgba(0x6750A4), OnPrimary: rgba(0xFFFFFF),
			PrimaryContainer: rgba(0xEADDFF), OnPrimaryContainer: rgba(0x21005D),
			Secondary: rgba(0x625B71), OnSecondary: rgba(0xFFFFFF),
			SecondaryContainer: rgba(0xE8DEF8), OnSecondaryContainer: rgba(0x1D192B),
			Tertiary: rgba(0x7D5260), OnTertiary: rgba(0xFFFFFF),
			TertiaryContainer: rgba(0xFFD8E4), OnTertiaryContainer: rgba(0x31111D),
			Error: rgba(0xB3261E), OnError: rgba(0xFFFFFF),
			ErrorContainer: rgba(0xF9DEDC), OnErrorContainer: rgba(0x410E0B),

			Surface: rgba(0xFEF7FF), OnSurface: rgba(0x1D1B20),
			SurfaceVariant: rgba(0xE7E0EC), OnSurfaceVariant: rgba(0x49454F),
			SurfaceDim: rgba(0xDED8E1), SurfaceBright: rgba(0xFEF7FF),
			SurfaceContainerLowest:  rgba(0xFFFFFF),
			SurfaceContainerLow:     rgba(0xF7F2FA),
			SurfaceContainer:        rgba(0xF3EDF7),
			SurfaceContainerHigh:    rgba(0xECE6F0),
			SurfaceContainerHighest: rgba(0xE6E0E9),

			Outline: rgba(0x79747E), OutlineVariant: rgba(0xCAC4D0),
			InverseSurface: rgba(0x322F35), InverseOnSurface: rgba(0xF5EFF7),
			InversePrimary: rgba(0xD0BCFF),
			Shadow:         rgba(0x000000), Scrim: rgba(0x000000),
		}.init(),
		Type: defaultType(),
	}
}

// Dark is the Material 3 baseline dark scheme.
func Dark() *Theme {
	return &Theme{
		Name: "baseline-dark",
		Dark: true,
		Colors: Colors{
			Primary: rgba(0xD0BCFF), OnPrimary: rgba(0x381E72),
			PrimaryContainer: rgba(0x4F378B), OnPrimaryContainer: rgba(0xEADDFF),
			Secondary: rgba(0xCCC2DC), OnSecondary: rgba(0x332D41),
			SecondaryContainer: rgba(0x4A4458), OnSecondaryContainer: rgba(0xE8DEF8),
			Tertiary: rgba(0xEFB8C8), OnTertiary: rgba(0x492532),
			TertiaryContainer: rgba(0x633B48), OnTertiaryContainer: rgba(0xFFD8E4),
			Error: rgba(0xF2B8B5), OnError: rgba(0x601410),
			ErrorContainer: rgba(0x8C1D18), OnErrorContainer: rgba(0xF9DEDC),

			Surface: rgba(0x141218), OnSurface: rgba(0xE6E0E9),
			SurfaceVariant: rgba(0x49454F), OnSurfaceVariant: rgba(0xCAC4D0),
			SurfaceDim: rgba(0x141218), SurfaceBright: rgba(0x3B383E),
			SurfaceContainerLowest:  rgba(0x0F0D13),
			SurfaceContainerLow:     rgba(0x1D1B20),
			SurfaceContainer:        rgba(0x211F26),
			SurfaceContainerHigh:    rgba(0x2B2930),
			SurfaceContainerHighest: rgba(0x36343B),

			Outline: rgba(0x938F99), OutlineVariant: rgba(0x49454F),
			InverseSurface: rgba(0xE6E0E9), InverseOnSurface: rgba(0x322F35),
			InversePrimary: rgba(0x6750A4),
			Shadow:         rgba(0x000000), Scrim: rgba(0x000000),
		}.init(),
		Type: defaultType(),
	}
}

// init fills the fields that are easy to confuse, so a missing role cannot
// silently render black.
func (c Colors) init() Colors { return c }

// TextStyle is one step of the type scale.
type TextStyle struct {
	Size          float64
	LineHeight    float64
	Weight        int
	LetterSpacing float64
}

// TypeScale is the Material 3 type scale.
type TypeScale struct {
	DisplayLarge, DisplayMedium, DisplaySmall    TextStyle
	HeadlineLarge, HeadlineMedium, HeadlineSmall TextStyle
	TitleLarge, TitleMedium, TitleSmall          TextStyle
	BodyLarge, BodyMedium, BodySmall             TextStyle
	LabelLarge, LabelMedium, LabelSmall          TextStyle
}

func defaultType() TypeScale {
	return TypeScale{
		DisplayLarge:   TextStyle{57, 64, 400, -0.25},
		DisplayMedium:  TextStyle{45, 52, 400, 0},
		DisplaySmall:   TextStyle{36, 44, 400, 0},
		HeadlineLarge:  TextStyle{32, 40, 400, 0},
		HeadlineMedium: TextStyle{28, 36, 400, 0},
		HeadlineSmall:  TextStyle{24, 32, 400, 0},
		TitleLarge:     TextStyle{22, 28, 400, 0},
		TitleMedium:    TextStyle{16, 24, 500, 0.15},
		TitleSmall:     TextStyle{14, 20, 500, 0.1},
		BodyLarge:      TextStyle{16, 24, 400, 0.5},
		BodyMedium:     TextStyle{14, 20, 400, 0.25},
		BodySmall:      TextStyle{12, 16, 400, 0.4},
		LabelLarge:     TextStyle{14, 20, 500, 0.1},
		LabelMedium:    TextStyle{12, 16, 500, 0.5},
		LabelSmall:     TextStyle{11, 16, 500, 0.5},
	}
}

// RoleStyle resolves an M3 type-scale role name for text. Unknown names fall
// back to bodyMedium, which is the default role of a Text element.
func RoleStyle(role string) TextStyle {
	t := Light().Type
	switch role {
	case "displayLarge":
		return t.DisplayLarge
	case "displayMedium":
		return t.DisplayMedium
	case "displaySmall":
		return t.DisplaySmall
	case "headlineLarge":
		return t.HeadlineLarge
	case "headlineMedium":
		return t.HeadlineMedium
	case "headlineSmall":
		return t.HeadlineSmall
	case "titleLarge":
		return t.TitleLarge
	case "titleMedium":
		return t.TitleMedium
	case "titleSmall":
		return t.TitleSmall
	case "bodyLarge":
		return t.BodyLarge
	case "bodySmall":
		return t.BodySmall
	case "labelLarge":
		return t.LabelLarge
	case "labelMedium":
		return t.LabelMedium
	case "labelSmall":
		return t.LabelSmall
	}
	return t.BodyMedium
}

// Shape is the corner radius scale, in logical pixels. Full means "half the
// shortest side", which components clamp themselves.
type ShapeScale struct{}

// Shape radii.
const (
	ShapeNone       = 0.0
	ShapeExtraSmall = 4.0
	ShapeSmall      = 8.0
	ShapeMedium     = 12.0
	ShapeLarge      = 16.0
	ShapeExtraLarge = 28.0
	// ShapeFull means the corner radius is half the shortest side.
	ShapeFull = -1.0
)

// Elevation levels, in dp, used for shadows.
const (
	Elevation0 = 0
	Elevation1 = 1
	Elevation2 = 3
	Elevation3 = 6
	Elevation4 = 8
	Elevation5 = 12
)

// StateLayerOpacities are the Material 3 state layer opacities.
const (
	StateHover   = 0.08
	StateFocus   = 0.10
	StatePressed = 0.10
	StateDragged = 0.16
)

// SelectionOpacity is the alpha of the text selection highlight.
const SelectionOpacity = 0.4

// DisabledOpacity is applied to the whole disabled component.
const DisabledOpacity = 0.38

// DisabledContainerOpacity is used for disabled container fills.
const DisabledContainerOpacity = 0.12
