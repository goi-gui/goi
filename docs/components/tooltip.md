# Tooltip

A short hint attached to a control.

[All components](../components.md)

## In a `.goi` file

```goi
Tooltip { label: "Saves to your library" width: 200 }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `variant` | enum | visual style; see the page |


## From Go

```go
tip := ui.MustByID[*ui.Tooltip](w, "tip")
tip.SetVisible(true)
```

Every element also accepts the [common properties](../components.md#common-properties).
