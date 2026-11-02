# Row

Places children left to right.

[All components](../components.md)

## In a `.goi` file

```goi
Row {
	spacing: 8
	crossAxisAlign: center
	Button { label: "Save" }
	Button { label: "Cancel" variant: text }
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
row := ui.MustByID[*ui.Row](w, "actions")
row.Spacing.Set(16)
```

Every element also accepts the [common properties](../components.md#common-properties).
