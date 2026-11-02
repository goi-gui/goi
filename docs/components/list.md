# List

A vertical container for `ListItem`s and `MenuItem`s.

[All components](../components.md)

## In a `.goi` file

```goi
List {
	id: results
	spacing: 4
	ListItem { label: "Inbox" icon: inbox }
	ListItem { label: "Drafts" icon: draft }
}
```

For rows that come from Go, keep one hidden prototype and fill from a slice:

```goi
List {
	id: results
	ListItem { template: true label: "Item" }
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
list := ui.MustByID[*ui.List](w, "results")
err := ui.Fill(w, list, results, func(row *ui.ListItem, r result) {
	row.Label.Set(r.Title)
	row.Supporting.Set(r.Subtitle)
})
```

`Fill` removes the previous rows first, so a refresh cannot stack duplicates.

Every element also accepts the [common properties](../components.md#common-properties).
