# MenuItem

One row in a menu.

[All components](../components.md)

## In a `.goi` file

```goi
MenuItem { label: "Cut" icon: content_cut }
MenuItem { label: "Delete" icon: delete disabled: true }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `icon` | string | leading Material Symbols ligature |
| `trailingIcon` | string | trailing Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |
| `selected` | bool | active/selection state |


## From Go

```go
item := ui.MustByID[*ui.MenuItem](w, "cut")
item.OnClick(func(ui.ClickEvent) {
	cut()
	ui.MustByID[*ui.Menu](w, "menu").SetVisible(false)
})
```

Every element also accepts the [common properties](../components.md#common-properties).
