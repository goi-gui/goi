# Badge

A small count or status dot, usually overlaid on an icon with `Positioned` or `Stack`.

[All components](../components.md)

## In a `.goi` file

```goi
Badge { }
Badge { label: "12" }
Badge { label: "99+" }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |

## From Go

```go
badge := ui.MustByID[*ui.Badge](w, "unread")
badge.Label.Set(strconv.Itoa(count))
```

Every element also accepts the [common properties](../components.md#common-properties).
