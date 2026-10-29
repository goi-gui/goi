package text

import _ "embed"

// The embedded fonts are the ones Material 3 uses: Roboto for text and
// Material Symbols Outlined for icons. Both are OFL/Apache licensed and are
// stored as woff, which the shaper parses directly.
var (
	//go:embed fonts/roboto-light.woff
	robotoLight []byte
	//go:embed fonts/roboto-regular.woff
	robotoRegular []byte
	//go:embed fonts/roboto-medium.woff
	robotoMedium []byte
	//go:embed fonts/roboto-bold.woff
	robotoBold []byte
	//go:embed fonts/roboto-black.woff
	robotoBlack []byte
	//go:embed fonts/material-symbols.woff
	materialSymbols []byte
)

// Weight is a Roboto weight. The M3 type scale uses regular, medium and bold;
// light and black are here for content that needs a wider range.
type Weight int

// Font weights.
const (
	WeightLight   Weight = 300
	WeightRegular Weight = 400
	WeightMedium  Weight = 500
	WeightBold    Weight = 700
	WeightBlack   Weight = 900
)

// NearestWeight snaps an arbitrary CSS-style weight to a face the library
// loads. Layout and paint both call it, so a declared weight of 600 measures and
// draws with the same face instead of falling back to regular in one and bold in
// the other.
func NearestWeight(w Weight) Weight {
	switch {
	case w < 350:
		return WeightLight
	case w < 450:
		return WeightRegular
	case w < 600:
		return WeightMedium
	case w < 800:
		return WeightBold
	}
	return WeightBlack
}
