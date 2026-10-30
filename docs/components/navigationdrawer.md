# NavigationDrawer

A modal drawer that lists the same destinations as the rail and bar.

[All components](../components.md)

## In a `.ui` file

```ui
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

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
drawer := ui.MustByID[*ui.NavigationDrawer](w, "drawer")
drawer.OnClick(func(ui.ClickEvent) { closeDrawer() })
```

Every element also accepts the [common properties](../components.md#common-properties).
