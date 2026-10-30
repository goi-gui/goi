# NavigationRail

The side navigation rail for medium and expanded windows.

[All components](../components.md)

## In a `.ui` file

```ui
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

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
rail := ui.MustByID[*ui.NavigationRail](w, "rail")
rail.OnSelect(func(i int) { show(i) })

// Mark one destination without an index argument.
for i, kid := range rail.Base().Kids() {
	if kid.Node().Kind() == "NavigationItem" {
		kid.Node().(interface{ M3() *ui.Component }).M3().Selected.Set(i == active)
	}
}
```

Every element also accepts the [common properties](../components.md#common-properties).
