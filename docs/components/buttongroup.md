# ButtonGroup

A row of related buttons that share one shape and one gap.

[All components](../components.md)

## In a `.goi` file

```goi
ButtonGroup {
	spacing: 8
	Button { label: "Day" variant: outlined }
	Button { label: "Week" variant: outlined }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `disabled` | bool | dims the element and blocks input |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `crossAxisAlign` | enum | `start`, `center`, `end`, `stretch`; places a child across the flow |
| `mainAxisAlign` | enum | `start`, `center`, `end`, `spaceBetween`, `spaceAround`, `spaceEvenly`; places the run along the flow |

## From Go

```go
group := ui.MustByID[*ui.ButtonGroup](w, "group")
group.Spacing.Set(12)
```

Every element also accepts the [common properties](../components.md#common-properties).
