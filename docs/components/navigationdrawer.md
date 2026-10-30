# NavigationDrawer

A modal drawer that lists the same destinations as the rail and bar.

[All components](../components.md)

## In a `.goi` file

```goi
NavigationDrawer {
	label: "Categories"
	NavigationItem { label: "Home" icon: home selected: true }
	NavigationItem { label: "Search" icon: search }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `variant` | enum | visual style; see the page |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `align` | enum | cross-axis placement |

## From Go

```go
drawer := ui.MustByID[*ui.NavigationDrawer](w, "drawer")
drawer.OnClick(func(ui.ClickEvent) { closeDrawer() })
```

Every element also accepts the [common properties](../components.md#common-properties).
