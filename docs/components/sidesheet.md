# SideSheet

A surface anchored to a side edge, for settings or detail.

[All components](../components.md)

## In a `.goi` file

```goi
SideSheet { label: "Filters" width: 320 height: 480 }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `supporting` | string | secondary line under the label |
| `variant` | enum | visual style; see the page |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `align` | enum | cross-axis placement |

## From Go

```go
side := ui.MustByID[*ui.SideSheet](w, "filters")
side.SetVisible(true)
```

Every element also accepts the [common properties](../components.md#common-properties).
