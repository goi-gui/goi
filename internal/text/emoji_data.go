//go:build !noemoji

package text

import _ "embed"

//go:embed fonts/noto-color-emoji.ttf
var notoEmoji []byte
