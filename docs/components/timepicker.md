# TimePicker

A clock dial for picking a time.

[All components](../components.md)

## In a `.goi` file

```goi
TimePicker { id: time label: "Select time" text: "10:30" }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the caption above the time |
| `text` | string | the picked time as `HH:MM` (24h); empty means now |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `align` | enum | cross-axis placement |

## Behaviour

The dial starts on the hour: a click sets the hour and hands over to the
minutes, so a second click completes the time. The two halves of the big time in
the header are targets too, so either half can be picked first, and the active
half is drawn in `primary`.

## From Go

```go
picker := ui.MustByID[*ui.TimePicker](w, "time")
picker.OnSubmit(func(v string) {
	fmt.Println("picked", v) // "10:30"
})
picker.Text.Set("10:30")
```

Every element also accepts the [common properties](../components.md#common-properties).
