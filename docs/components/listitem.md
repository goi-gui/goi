# ListItem

One row in a list: leading icon, label, optional supporting line, trailing affordance.

[All components](../components.md)

## In a `.ui` file

```ui
ListItem { label: "Inbox" icon: inbox }
ListItem { label: "Two lines" supporting: "Supporting text" icon: photo }
ListItem { label: "Selected" icon: star selected: true }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `supporting` | string | secondary line under the label |
| `icon` | string | leading Material Symbols ligature |
| `trailingIcon` | string | trailing Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |
| `selected` | bool | active/selection state |


## From Go

```go
row := ui.MustByID[*ui.ListItem](w, "item")
row.OnClick(func(ui.ClickEvent) {
	row.Selected.Set(true)
})
```

Every element also accepts the [common properties](../components.md#common-properties).
