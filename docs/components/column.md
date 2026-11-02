# Column

Stacks children vertically, top to bottom.

[All components](../components.md)

## In a `.goi` file

```goi
Column {
	spacing: 12
	padding: 16
	align: stretch
	Text { text: "Title" role: "titleMedium" }
	Text { text: "Body" role: "bodyMedium" }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `align` | enum | cross-axis placement |

## From Go

A column is also the usual list: mark one row `template: true` and let
`ui.Fill` replace the rows from a slice.

```goi
Column {
	id: list
	spacing: 8
	ListItem { template: true label: "Item" }
}
```

```go
col := ui.MustByID[*ui.Column](w, "list")
err := ui.Fill(w, col, items, func(row *ui.ListItem, it item) {
	row.Label.Set(it.Label)
})
```

`ui.Instantiate[*ui.ListItem](w, "template", parent)` clones the row once when
you need to add a single item by hand.

Every element also accepts the [common properties](../components.md#common-properties).
