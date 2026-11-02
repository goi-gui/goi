# Code

A syntax-highlighted code block.

[All components](../components.md)

## In a `.goi` file

```goi
Code {
	language: go
	text: "fmt.Println(\"hello\")\nreturn nil"
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `text` | string | the source; `\n` separates lines |
| `language` | string | any of the ~300 lexer names Chroma knows: `go`, `goi`, `js`, `python`, `sh`, `yaml`, `sql`, `rust`, `html`, `css`, `toml`, …; anything else reads as plain |
| `showCopy` | bool | draws a copy button in the top-right corner |
| `variant` | enum | `filled` (default) or `outlined` |

## Behaviour

Lines break only on `\n`, never at the box edge: a code block keeps its shape
and a line too long for the box is clipped. The height follows the line count,
so no `height` is needed. Tabs expand to four spaces.

Highlighting colors keywords and function names with `primary`, strings and
attributes with `tertiary`, numbers and type names with `secondary`, and
comments with `onSurfaceVariant`, so the block follows the theme. The lexing is
[Chroma](https://github.com/alecthomas/chroma)'s, so every language it ships
works, including template syntax inside a string.

The text is selectable like prose: drag to select, double click for a word,
triple click for the line's paragraph, Ctrl+C to copy. `showCopy` adds a button
that copies the whole block.

Tabs are expanded to four spaces, because a tab has no advance of its own in the
font. What you see is what is copied, so a selected snippet comes out with
spaces.

## From Go

```go
block := ui.MustByID[*ui.Code](w, "snippet")
block.SetText(source)
block.Language.Set("python")
```

Every element also accepts the [common properties](../components.md#common-properties).
