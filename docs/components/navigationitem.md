# NavigationItem

One destination in a navigation bar, rail or drawer.

[All components](../components.md)

## In a `.ui` file

```ui
NavigationItem { label: "Home" icon: home selected: true }
NavigationItem { label: "Search" icon: search }
NavigationItem { label: "Locked" icon: lock disabled: true }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `icon` | string | leading Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |
| `selected` | bool | active/selection state |


## From Go

```go
item := ui.MustByID[*ui.NavigationItem](w, "search")
item.OnClick(func(ui.ClickEvent) { show(searchPage) })
item.M3().Selected.Set(true)
```

Every element also accepts the [common properties](../components.md#common-properties).
