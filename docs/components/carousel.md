# Carousel

A horizontally scrolling row of cards.

[All components](../components.md)

## In a `.ui` file

```ui
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

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
carousel := ui.MustByID[*ui.Carousel](w, "carousel")
carousel.Spacing.Set(16)
```

Every element also accepts the [common properties](../components.md#common-properties).
