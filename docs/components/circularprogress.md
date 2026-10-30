# CircularProgress

A circular progress ring.

[All components](../components.md)

## In a `.ui` file

```ui
CircularProgress { value: 0.3 width: 64 height: 64 }
CircularProgress { value: 70 min: 0 max: 100 width: 48 height: 48 }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `value` | number | current number |
| `min` | number | lowest value |
| `max` | number | highest value |
| `size` | enum | M3 size token |


## From Go

```go
ring := ui.MustByID[*ui.CircularProgress](w, "ring")
app.Animate(&ring.Value, 0.9, 900*time.Millisecond, ui.Emphasized)
```

Every element also accepts the [common properties](../components.md#common-properties).
