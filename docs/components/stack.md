# Stack

Overlays children on top of each other, last child on top. It is the base for dialogs, scrims and badges.

[All components](../components.md)

## In a `.goi` file

```goi
Stack {
	flex: 1
	Rectangle { background: surfaceContainer radius: 16 }
	Positioned { right: 12 bottom: 12
		FAB { icon: edit }
	}
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `align` | enum | cross-axis placement |


## From Go

```go
stack := ui.MustByID[*ui.Stack](w, "overlay")
sheet, err := ui.ByID[*ui.Positioned](w, "sheet")
if err == nil {
	sheet.SetVisible(true)
}
```

Every element also accepts the [common properties](../components.md#common-properties).
