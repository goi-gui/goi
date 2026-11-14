//go:build noemoji

package text

// notoEmoji is absent in a noemoji build; text falls back to the mono glyphs
// when the face has them and to nothing otherwise.
var notoEmoji []byte
