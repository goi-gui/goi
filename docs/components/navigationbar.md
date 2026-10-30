# NavigationBar

The bottom navigation bar for the compact window class.

[All components](../components.md)

## In a `.goi` file

```goi
NavigationBar {
	id: navbar
	visible: { base: true, sm: false }
	NavigationItem { label: "Home" icon: home selected: true }
	NavigationItem { label: "Search" icon: search }
	NavigationItem { label: "Profile" icon: person }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `align` | enum | cross-axis placement |

## From Go

```go
bar := ui.MustByID[*ui.NavigationBar](w, "navbar")
bar.OnSelect(func(i int) { show(i) })
```

Every element also accepts the [common properties](../components.md#common-properties).
