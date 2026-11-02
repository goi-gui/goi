# Toolbar

A horizontal strip of compact controls.

[All components](../components.md)

## In a `.goi` file

```goi
Toolbar {
	spacing: 4
	IconButton { icon: format_bold }
	IconButton { icon: format_italic }
	Divider { variant: vertical }
	IconButton { icon: format_underlined }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `crossAxisAlign` | enum | `start`, `center`, `end`, `stretch`; places a child across the flow |
| `mainAxisAlign` | enum | `start`, `center`, `end`, `spaceBetween`, `spaceAround`, `spaceEvenly`; places the run along the flow |

## From Go

```go
bar := ui.MustByID[*ui.Toolbar](w, "format")
bar.Spacing.Set(2)
```

Every element also accepts the [common properties](../components.md#common-properties).
