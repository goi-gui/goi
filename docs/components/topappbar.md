# TopAppBar

The bar at the top of a screen: a leading icon, a title and a trailing icon.

[All components](../components.md)

## In a `.goi` file

```goi
TopAppBar {
	id: appbar
	label: "Inbox"
	icon: menu
	trailingIcon: search
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `variant` | enum | visual style; see the page |
| `icon` | string | leading Material Symbols ligature |
| `trailingIcon` | string | trailing Material Symbols ligature |


## From Go

```go
bar := ui.MustByID[*ui.TopAppBar](w, "appbar")
bar.Label.Set("Settings")
bar.OnClick(func(e ui.ClickEvent) {
	// e.X is a window coordinate; compare against the bar's own box for sides.
	if e.X < 56 {
		toggleDrawer()
	}
})
```

Every element also accepts the [common properties](../components.md#common-properties).
