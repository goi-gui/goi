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
| `variant` | enum | visual style; see the page |

## From Go

```go
div := ui.MustByID[*ui.Divider](w, "divider")
div.SetVisible(hasResults)
```

Every element also accepts the [common properties](../components.md#common-properties).
