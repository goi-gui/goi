# FABMenu

A FAB that expands into a column of actions.

[All components](../components.md)

## In a `.goi` file

```goi
FABMenu {
	id: actions
	icon: add
	expanded: false
	FAB { icon: edit variant: small }
	FAB { icon: photo variant: small }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `icon` | string | leading Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `disabled` | bool | dims the element and blocks input |
| `expanded` | bool | open state (menu, split button) |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `crossAxisAlign` | enum | `start`, `center`, `end`, `stretch`; places a child across the flow |
| `mainAxisAlign` | enum | `start`, `center`, `end`, `spaceBetween`, `spaceAround`, `spaceEvenly`; places the run along the flow |

While `expanded` is true the FAB sits at the bottom of its own box and the
actions stack above it, right-aligned with a 16dp gap, the last declared action
closest to the FAB. While it is closed the box is just the FAB and the actions
have no bounds at all, so they neither draw nor take a click.

## From Go

```go
menu := ui.MustByID[*ui.FABMenu](w, "actions")
menu.OnClick(func(ui.ClickEvent) {
	menu.Expanded.Set(!menu.Expanded.Get())
})
```

Every element also accepts the [common properties](../components.md#common-properties).
