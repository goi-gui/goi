# Slider

A continuous value control.

[All components](../components.md)

## In a `.goi` file

```goi
Slider { value: 0.4 min: 0 max: 1 width: 320 }
Slider { value: 40 min: 0 max: 100 step: 10 }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |
| `value` | number | current number |
| `min` | number | lowest value |
| `max` | number | highest value |
| `step` | number | snap interval; 0 means continuous |

## From Go

```go
vol := ui.MustByID[*ui.Slider](w, "volume")
vol.OnChangeValue(func(v float64) {
	fmt.Println("volume", v)
})
app.Animate(&vol.Value, 0.9, 700*time.Millisecond, ui.Standard)
```

Every element also accepts the [common properties](../components.md#common-properties).
