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

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `align` | enum | cross-axis placement |

## From Go

```go
list := ui.MustByID[*ui.List](w, "results")
for _, r := range results {
	item, err := ui.Instantiate[*ui.ListItem](w, "resultTemplate", list)
	if err != nil {
		continue
	}
	item.Label.Set(r.Title)
	item.Supporting.Set(r.Subtitle)
}
```

Every element also accepts the [common properties](../components.md#common-properties).
