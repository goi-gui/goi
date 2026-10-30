# Checkbox

A binary choice with an optional label.

[All components](../components.md)

## In a `.goi` file

```goi
Checkbox { label: "Remember me" checked: true }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |
| `checked` | bool | on/off state |

## From Go

```go
cb := ui.MustByID[*ui.Checkbox](w, "remember")
cb.OnToggle(func(v bool) { remember = v })
cb.Checked.Set(true)
```

Every element also accepts the [common properties](../components.md#common-properties).
