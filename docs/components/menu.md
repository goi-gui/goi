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

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
menu := ui.MustByID[*ui.Menu](w, "menu")
menu.SetVisible(!menu.Base().Visible())
```

Every element also accepts the [common properties](../components.md#common-properties).
