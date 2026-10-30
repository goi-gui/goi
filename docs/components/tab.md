# Tab

One tab in a tab bar.

[All components](../components.md)

## In a `.goi` file

```goi
Tab { label: "Overview" selected: true }
Tab { label: "Specs" icon: tune }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `icon` | string | leading Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |
| `selected` | bool | active/selection state |


## From Go

```go
tab := ui.MustByID[*ui.Tab](w, "specs")
tab.Selected.Set(true)
```

Every element also accepts the [common properties](../components.md#common-properties).
