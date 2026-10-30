# Row

Places children left to right.

[All components](../components.md)

## In a `.ui` file

```ui
Row {
	spacing: 8
	align: center
	Button { label: "Save" }
	Button { label: "Cancel" variant: text }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `align` | enum | cross-axis placement |


## From Go

```go
row := ui.MustByID[*ui.Row](w, "actions")
row.Spacing.Set(16)
```

Every element also accepts the [common properties](../components.md#common-properties).
