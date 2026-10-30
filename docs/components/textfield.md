# TextField

An editable single-line field with a floating label.

[All components](../components.md)

## In a `.ui` file

```ui
TextField { label: "Email" width: 320 }
TextField { label: "Name" text: "Ada" variant: outlined }
TextField { label: "Email" placeholder: "you@example.com" icon: mail }
TextField { label: "Notes" supporting: "Shown under the field" }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `text` | string | current value |
| `placeholder` | string | shown while the value is empty |
| `supporting` | string | secondary line under the label |
| `icon` | string | leading Material Symbols ligature |
| `trailingIcon` | string | trailing Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |


## From Go

```go
email := ui.MustByID[*ui.TextField](w, "email")
email.OnEdit(func(s string) { draft.Email = s })
email.OnSubmit(func(s string) { save(s) })
```

Every element also accepts the [common properties](../components.md#common-properties).
