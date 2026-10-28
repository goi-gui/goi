package text

import _ "embed"

// The embedded fonts are the ones Material 3 uses: Roboto for text and
// Material Symbols Outlined for icons. Both are OFL/Apache licensed and are
// stored as woff, which the shaper parses directly.
var (
	//go:embed fonts/roboto-regular.woff
	robotoRegular []byte
	//go:embed fonts/roboto-medium.woff
	robotoMedium []byte
	//go:embed fonts/roboto-bold.woff
	robotoBold []byte
	//go:embed fonts/material-symbols.woff
	materialSymbols []byte
)

// Weight is a Roboto weight. M3 only uses these three.
type Weight int

// Font weights.
const (
	WeightRegular Weight = 400
	WeightMedium  Weight = 500
	WeightBold    Weight = 700
)
