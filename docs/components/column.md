# Column

Stacks children vertically, top to bottom.

[All components](../components.md)

## In a `.ui` file

```ui
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

```go
col := ui.MustByID[*ui.Column](w, "list")
col.Spacing.Set(8)
for i := 0; i < 3; i++ {
	item, err := ui.Instantiate[*ui.ListItem](w, "itemTemplate", col)
	if err != nil {
		continue
	}
	item.Label.Set(fmt.Sprintf("Item %d", i+1))
}
```

Every element also accepts the [common properties](../components.md#common-properties).
