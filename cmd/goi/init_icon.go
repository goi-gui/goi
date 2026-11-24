//go:build !js && !wasip1

package main

import _ "embed"

// initIconPNG is the project icon goi init writes next to the design. goi pack
// reads it as the default icon: a project packs without any setup. It is the
// repository's icon.png converted to 8-bit RGBA, which is what every packager
// and desktop reads without surprises.
//
//go:embed init-icon.png
var initIconPNG []byte
