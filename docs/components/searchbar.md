# SearchBar

A text field shaped as a search input, with a leading search glyph.

[All components](../components.md)

## In a `.goi` file

```goi
SearchBar {
	label: "Search"
	placeholder: "Search components"
	width: 360
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `text` | string | current value |
| `placeholder` | string | shown while the value is empty |
| `icon` | string | leading Material Symbols ligature |
| `trailingIcon` | string | trailing Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |


## From Go

```go
search := ui.MustByID[*ui.SearchBar](w, "search")
search.OnEdit(func(q string) { filter(q) })
```

Every element also accepts the [common properties](../components.md#common-properties).
