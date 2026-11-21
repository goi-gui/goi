# Fonts

| File | Font | Licence |
|---|---|---|
| `roboto-*.woff` | Roboto | Apache License 2.0 |
| `roboto-variable.ttf` | Roboto, subset of `Roboto[wdth,wght]` | Apache License 2.0 |
| `roboto-italic-variable.ttf` | Roboto Italic, subset of `Roboto-Italic[wdth,wght]` | Apache License 2.0 |
| `material-symbols.woff` | Material Symbols Outlined | Apache License 2.0 |
| `material-symbols-filled.woff` | Material Symbols Outlined, `FILL` axis at 1 | Apache License 2.0 |
| `noto-color-emoji.ttf` | Noto Color Emoji | SIL Open Font License 1.1 |
| `roboto-mono.ttf` | Roboto Mono | Apache License 2.0 |
| `meslo-lg-nf-mono.ttf` | Meslo LG S NF Mono | Apache License 2.0 |

The two icon files are the same family, instanced from the variable font with
the `FILL` axis at 0 and at 1, so `filledIcon` switches the glyphs an icon draws
without a second icon set to name. They are embedded so a build draws text,
icons and colour emoji with no system font, no font configuration and no network. Noto Color Emoji is the same
font Android ships; deleting it drops ~11 MB from a binary, and emoji fall back to
the text face's monochrome glyphs.
