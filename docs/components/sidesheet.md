# SideSheet

A surface anchored to a side edge, for settings or detail.

[All components](../components.md)

## In a `.ui` file

```ui
SideSheet { label: "Filters" width: 320 height: 480 }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `supporting` | string | secondary line under the label |
| `variant` | enum | visual style; see the page |

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
side := ui.MustByID[*ui.SideSheet](w, "filters")
side.SetVisible(true)
```

Every element also accepts the [common properties](../components.md#common-properties).
