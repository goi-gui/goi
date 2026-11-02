# Tabs

A tab bar that groups `Tab` children.

[All components](../components.md)

## In a `.goi` file

```goi
Tabs {
	Tab { label: "Overview" selected: true }
	Tab { label: "Specs" }
	Tab { label: "Details" }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `crossAxisAlign` | enum | `start`, `center`, `end`, `stretch`; places a child across the flow |
| `mainAxisAlign` | enum | `start`, `center`, `end`, `spaceBetween`, `spaceAround`, `spaceEvenly`; places the run along the flow |

## From Go

```go
tabs := ui.MustByID[*ui.Tabs](w, "tabs")
tabs.OnSelect(func(i int) { showPanel(i) })
```

Every element also accepts the [common properties](../components.md#common-properties).
