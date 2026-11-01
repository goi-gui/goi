# Divider

A one-pixel rule between content.

[All components](../components.md)

## In a `.goi` file

```goi
Divider { }
Divider { variant: vertical }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | `horizontal` (default) or `vertical` |

A vertical divider is 1dp wide and takes the height it is given, so it needs one:
`Divider { variant: vertical height: 24 }` inside a `Toolbar` or a `Row`. The
horizontal form is 1dp tall and fills the width.

## From Go

```go
div := ui.MustByID[*ui.Divider](w, "divider")
div.SetVisible(hasResults)
```

Every element also accepts the [common properties](../components.md#common-properties).
