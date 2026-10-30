# Carousel

A horizontally scrolling row of cards.

[All components](../components.md)

## In a `.goi` file

```goi
Carousel {
	spacing: 12
	Card { label: "One" width: 200 height: 120 }
	Card { label: "Two" width: 200 height: 120 }
	Card { label: "Three" width: 200 height: 120 }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `align` | enum | cross-axis placement |

## From Go

```go
carousel := ui.MustByID[*ui.Carousel](w, "carousel")
carousel.Spacing.Set(16)
```

Every element also accepts the [common properties](../components.md#common-properties).
