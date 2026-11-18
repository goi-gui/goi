//go:build !all && !emoji

package text

// notoEmoji is absent without the `emoji` feature; text falls back to the mono glyphs
// when the face has them and to nothing otherwise.
var notoEmoji []byte
