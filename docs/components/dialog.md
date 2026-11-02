# Dialog

A modal surface for a decision or a short form.

[All components](../components.md)

## In a `.goi` file

```goi
Dialog {
	label: "Reset settings?"
	supporting: "This cannot be undone."
	width: 280
	height: 168
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `supporting` | string | secondary line under the label |
| `icon` | string | leading Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `crossAxisAlign` | enum | `start`, `center`, `end`, `stretch`; places a child across the flow |
| `mainAxisAlign` | enum | `start`, `center`, `end`, `spaceBetween`, `spaceAround`, `spaceEvenly`; places the run along the flow |

## From Go

```go
dialog := ui.MustByID[*ui.Dialog](w, "confirm")
scrim := ui.MustByID[*ui.Rectangle](w, "scrim")
open := func(v bool) {
	scrim.SetVisible(v)
	dialog.SetVisible(v)
}
w.OnKey(func(e ui.KeyEvent) {
	if e.Key == ui.KeyEscape {
		open(false)
	}
})
```

Every element also accepts the [common properties](../components.md#common-properties).
