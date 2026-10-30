# Button

The ordinary Material 3 button. Five variants share one element.

[All components](../components.md)

## In a `.ui` file

```ui
Button { label: "Filled" }
Button { label: "Tonal" variant: tonal }
Button { label: "Outlined" variant: outlined }
Button { label: "Text" variant: text }
Button { label: "Elevated" variant: elevated }
Button { label: "Save" icon: check variant: filled }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `icon` | string | leading Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `disabled` | bool | dims the element and blocks input |


## From Go

```go
btn := ui.MustByID[*ui.Button](w, "save")
btn.OnClick(func(ui.ClickEvent) {
	btn.Label.Set("Saved")
	btn.Disabled.Set(true)
})
```

Every element also accepts the [common properties](../components.md#common-properties).
