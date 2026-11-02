# NavigationRail

The side navigation rail for medium and expanded windows.

[All components](../components.md)

## In a `.goi` file

```goi
NavigationRail {
	id: rail
	visible: { base: false, sm: true }
	paddingBottom: 16
	NavigationItem { label: "Home" icon: home selected: true }
	NavigationItem { label: "Search" icon: search }
	// A rail's own actions sit at the bottom, under the destinations.
	Rectangle { flex: 1 }
	Switch { id: theme width: 52 alignSelf: center iconOn: dark_mode iconOff: light_mode }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `crossAxisAlign` | enum | `start`, `center`, `end`, `stretch`; places a child across the flow |
| `mainAxisAlign` | enum | `start`, `center`, `end`, `spaceBetween`, `spaceAround`, `spaceEvenly`; places the run along the flow |

## From Go

```go
rail := ui.MustByID[*ui.NavigationRail](w, "rail")
rail.OnSelect(func(i int) { show(i) })

// The index counts destinations only, so the theme switch below them does not
// shift the page list.
```

Every element also accepts the [common properties](../components.md#common-properties).
