# BottomSheet

A surface anchored to the bottom edge.

[All components](../components.md)

## In a `.goi` file

```goi
BottomSheet { label: "Share" width: 320 height: 160 }
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
sheet := ui.MustByID[*ui.BottomSheet](w, "sheet")
sheet.SetVisible(true)

// Animate the placement, not the node: a sheet that grows is usually a
// Positioned whose top edge moves.
frame := ui.MustByID[*ui.Positioned](w, "sheetFrame")
app.Animate(&frame.Top, 300, 250*time.Millisecond, ui.Emphasized)
```

Every element also accepts the [common properties](../components.md#common-properties).
