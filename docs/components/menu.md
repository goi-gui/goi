# Menu

A popup list of `MenuItem`s.

[All components](../components.md)

## In a `.goi` file

```goi
Menu {
	MenuItem { label: "Cut" icon: content_cut }
	MenuItem { label: "Copy" icon: content_copy }
	MenuItem { label: "Paste" icon: content_paste }
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
menu := ui.MustByID[*ui.Menu](w, "menu")
menu.SetVisible(!menu.Base().Visible())
```

Every element also accepts the [common properties](../components.md#common-properties).
