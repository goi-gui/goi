# Dialog

A modal surface for a decision or a short form.

[All components](../components.md)

## In a `.ui` file

```ui
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

As a container it also accepts `spacing`/`gap` and `align`.

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
