# TimePicker

A clock dial for picking a time.

[All components](../components.md)

## In a `.goi` file

```goi
TimePicker { label: "Select time" width: 328 height: 420 }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
picker := ui.MustByID[*ui.TimePicker](w, "time")
picker.SetVisible(true)
```

Every element also accepts the [common properties](../components.md#common-properties).
