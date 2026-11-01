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
| `language` | string | `go`, `goi`, `js`, `python`, `sh`, `json`; unknown reads as plain |
| `variant` | enum | `filled` (default) or `outlined` |

## Behaviour

Lines break only on `\n`, never at the box edge: a code block keeps its shape
and a line too long for the box is clipped. The height follows the line count,
so no `height` is needed. Tabs expand to four spaces.

Highlighting colors keywords with `primary`, strings with `tertiary`, numbers
with `secondary` and comments with `onSurfaceVariant`, so the block follows the
theme. It is a tokenizer, not a parser: brackets are not matched and template
syntax is not understood.

## From Go

```go
block := ui.MustByID[*ui.Code](w, "snippet")
block.SetText(source)
block.Language.Set("python")
```

Every element also accepts the [common properties](../components.md#common-properties).
