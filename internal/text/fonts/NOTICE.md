# Fonts

| File | Font | Licence |
|---|---|---|
| `roboto-*.woff` | Roboto | Apache License 2.0 |
| `material-symbols.woff` | Material Symbols Outlined | Apache License 2.0 |
| `noto-color-emoji.ttf` | Noto Color Emoji | SIL Open Font License 1.1 |

The fonts are embedded so a build draws text, icons and colour emoji with no
system font, no font configuration and no network. Noto Color Emoji is the same
font Android ships; deleting it drops ~11 MB from a binary, and emoji fall back to
the text face's monochrome glyphs.
