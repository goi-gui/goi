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
| `selected` | bool | active/selection state |
| `checked` | bool | on/off state |

## From Go

```go
chip := ui.MustByID[*ui.Chip](w, "filter")
chip.OnClick(func(ui.ClickEvent) {
	chip.Selected.Set(!chip.Selected.Get())
})
```

Every element also accepts the [common properties](../components.md#common-properties).
