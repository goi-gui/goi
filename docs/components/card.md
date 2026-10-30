# Card

A Material 3 surface that groups related content.

[All components](../components.md)

## In a `.ui` file

```ui
Card {
	label: "Elevated"
	supporting: "Surface container low"
	variant: elevated
	width: 240
}
Card { variant: filled width: 240 height: 88 }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `supporting` | string | secondary line under the label |
| `icon` | string | leading Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
card := ui.MustByID[*ui.Card](w, "summary")
card.Label.Set("3 results")
card.OnClick(func(ui.ClickEvent) { open(3) })
```

Every element also accepts the [common properties](../components.md#common-properties).
