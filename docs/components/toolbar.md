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

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
bar := ui.MustByID[*ui.Toolbar](w, "format")
bar.Spacing.Set(2)
```

Every element also accepts the [common properties](../components.md#common-properties).
