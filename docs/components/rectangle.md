# Rectangle

A plain colored box. It has no component behaviour, so it is the cheapest background, scrim or hit area.

[All components](../components.md)

## In a `.goi` file

```goi
Rectangle {
	background: primary
	radius: 8
	height: 4
}
```

## Properties

_No component-specific properties._

## From Go

```go
scrim := ui.MustByID[*ui.Rectangle](w, "scrim")
scrim.OnClick(func(ui.ClickEvent) { closeSheet() })
```

Every element also accepts the [common properties](../components.md#common-properties).
