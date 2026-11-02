# Wrap

Flows children left to right and starts a new run when the next child does not fit.

[All components](../components.md)

## In a `.goi` file

```goi
Wrap {
	spacing: 8
	Chip { label: "One" }
	Chip { label: "Two" }
	Chip { label: "Three" }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `crossAxisAlign` | enum | `start`, `center`, `end`, `stretch`; places a child across the flow |
| `mainAxisAlign` | enum | `start`, `center`, `end`, `spaceBetween`, `spaceAround`, `spaceEvenly`; places the run along the flow |

## From Go

```go
wrap := ui.MustByID[*ui.Wrap](w, "chips")
wrap.Spacing.Set(12)
```

Every element also accepts the [common properties](../components.md#common-properties).
