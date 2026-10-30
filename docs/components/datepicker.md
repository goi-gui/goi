# DatePicker

A calendar surface for picking a date.

[All components](../components.md)

## In a `.goi` file

```goi
DatePicker { label: "Select date" width: 328 height: 420 }
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
picker := ui.MustByID[*ui.DatePicker](w, "date")
picker.OnClick(func(e ui.ClickEvent) {
	fmt.Println("date cell pressed at", e.X, e.Y)
})
```

Every element also accepts the [common properties](../components.md#common-properties).
