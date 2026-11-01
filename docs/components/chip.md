# Chip

A compact, rounded element for filters, inputs, suggestions and tags.

[All components](../components.md)

## In a `.goi` file

```goi
Chip { label: "Assist" icon: event }
Chip { label: "Filter" variant: filter selected: true }
Chip { label: "Input" variant: input }
Chip { label: "Suggestion" variant: suggestion }
Chip { label: "Tag" trailingIcon: close }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `icon` | string | leading Material Symbols ligature |
| `trailingIcon` | string | trailing Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `disabled` | bool | dims the element and blocks input |
| `selected` | bool | active/selection state; shows a check in place of the leading icon |
| `checked` | bool | on/off state |

## From Go

```go
chip := ui.MustByID[*ui.Chip](w, "filter")
chip.OnClick(func(ui.ClickEvent) {
	chip.Selected.Set(!chip.Selected.Get())
})
```

A chip is 32dp tall and pads 16dp at each end. A leading icon (or the check a
selected chip shows instead) sits 8dp from the edge with an 8dp gap, and a
`trailingIcon` costs the same on the other side, so the label box always ends
inside the pill. Selecting a chip therefore relayouts it: the check needs the
same room an icon does.

Every element also accepts the [common properties](../components.md#common-properties).
