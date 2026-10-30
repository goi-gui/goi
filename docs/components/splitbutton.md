# SplitButton

A primary action with an attached menu trigger.

[All components](../components.md)

## In a `.ui` file

```ui
SplitButton {
	label: "Split"
	icon: add
	expanded: false
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `icon` | string | leading Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `disabled` | bool | dims the element and blocks input |
| `expanded` | bool | open state (menu, split button) |


## From Go

```go
split := ui.MustByID[*ui.SplitButton](w, "split")
split.OnClick(func(ui.ClickEvent) {
	split.Expanded.Set(!split.Expanded.Get())
})
```

Every element also accepts the [common properties](../components.md#common-properties).
