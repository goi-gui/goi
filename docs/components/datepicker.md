# DatePicker

A calendar surface for picking a date.

[All components](../components.md)

## In a `.goi` file

```goi
DatePicker { id: date label: "Select date" text: "2026-11-02" }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the caption above the calendar |
| `text` | string | the picked date as `YYYY-MM-DD`; empty means none |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `crossAxisAlign` | enum | `start`, `center`, `end`, `stretch`; places a child across the flow |
| `mainAxisAlign` | enum | `start`, `center`, `end`, `spaceBetween`, `spaceAround`, `spaceEvenly`; places the run along the flow |

## Behaviour

The month shown is the month of the value; the arrows in the header move it one
month at a time and leave the value alone. A click on a day cell sets `text` to
that day's ISO date. Today is outlined and the picked day is filled.

The picker draws its own grid, so the cells are not nodes: the click is mapped
back through the same geometry the painter used (`paint.DateLayoutOf`).

## From Go

```go
picker := ui.MustByID[*ui.DatePicker](w, "date")
picker.OnSubmit(func(v string) {
	fmt.Println("picked", v) // "2026-11-02"
})
picker.Text.Set("2026-11-02")
```

Every element also accepts the [common properties](../components.md#common-properties).
