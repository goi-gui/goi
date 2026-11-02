# Grid

Places children in a fixed number of equal-width columns.

[All components](../components.md)

## In a `.goi` file

```goi
Grid {
	columns: 3
	spacing: 12
	Card { label: "One" }
	Card { label: "Two" }
	Card { label: "Three" }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `columns` | number | number of equal-width columns |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `crossAxisAlign` | enum | `start`, `center`, `end`, `stretch`; places a child across the flow |
| `mainAxisAlign` | enum | `start`, `center`, `end`, `spaceBetween`, `spaceAround`, `spaceEvenly`; places the run along the flow |

## From Go

```go
grid := ui.MustByID[*ui.Grid](w, "grid")
grid.Columns.Set(2)
```

Every element also accepts the [common properties](../components.md#common-properties).
