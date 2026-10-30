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

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
menu := ui.MustByID[*ui.FABMenu](w, "actions")
menu.OnClick(func(ui.ClickEvent) {
	menu.Expanded.Set(!menu.Expanded.Get())
})
```

Every element also accepts the [common properties](../components.md#common-properties).
