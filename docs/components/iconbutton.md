# IconButton

A square button that holds only an icon.

[All components](../components.md)

## In a `.goi` file

```goi
IconButton { icon: favorite }
IconButton { icon: favorite variant: filled selected: true }
IconButton { icon: favorite variant: tonal }
IconButton { icon: favorite variant: outlined }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `icon` | string | leading Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `disabled` | bool | dims the element and blocks input |
| `selected` | bool | active/selection state |

## From Go

```go
star := ui.MustByID[*ui.IconButton](w, "star")
star.OnClick(func(ui.ClickEvent) {
	on := !star.Selected.Get()
	star.Selected.Set(on)
	if on {
		star.Icon.Set("star")
	} else {
		star.Icon.Set("star_border")
	}
})
```

Every element also accepts the [common properties](../components.md#common-properties).
