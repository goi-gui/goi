# Positioned

Anchors a child inside a Stack. Only valid as a direct child of `Stack`.

[All components](../components.md)

## In a `.ui` file

```ui
Stack {
	height: 200
	Positioned { left: 16 top: 16
		Card { label: "Top left" width: 160 }
	}
	Positioned { right: 16 bottom: 16
		Chip { label: "Bottom right" }
	}
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `left` | number | distance from the stack's left edge; `-1` is auto |
| `top` | number | distance from the stack's top edge; `-1` is auto |
| `right` | number | anchor to the right edge; `-1` is auto |
| `bottom` | number | anchor to the bottom edge; `-1` is auto |
| `translateX` | number | post-anchor horizontal shift, for enter/exit motion |
| `translateY` | number | post-anchor vertical shift |


## From Go

```go
frame := ui.MustByID[*ui.Positioned](w, "drawer")
frame.TranslateX.Set(-320)
stop := app.Animate(&frame.TranslateX, 0, 250*time.Millisecond, ui.Emphasized)
_ = stop
```

Every element also accepts the [common properties](../components.md#common-properties).
