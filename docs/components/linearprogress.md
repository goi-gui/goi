# LinearProgress

A horizontal progress bar.

[All components](../components.md)

## In a `.ui` file

```ui
LinearProgress { value: 0.65 }
LinearProgress { value: 30 min: 0 max: 100 }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `value` | number | current number |
| `min` | number | lowest value |
| `max` | number | highest value |


## From Go

```go
bar := ui.MustByID[*ui.LinearProgress](w, "upload")
app.Animate(&bar.Value, 1, 900*time.Millisecond, ui.Standard)
```

Every element also accepts the [common properties](../components.md#common-properties).
