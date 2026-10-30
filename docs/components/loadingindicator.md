# LoadingIndicator

The M3 indeterminate loading shape.

[All components](../components.md)

## In a `.goi` file

```goi
LoadingIndicator { width: 48 height: 48 }
LoadingIndicator { size: small }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |

## From Go

```go
spinner := ui.MustByID[*ui.LoadingIndicator](w, "loading")
spinner.SetVisible(true)
```

Every element also accepts the [common properties](../components.md#common-properties).
