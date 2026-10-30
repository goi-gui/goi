# FAB

The floating action button: the primary action of a page.

[All components](../components.md)

## In a `.goi` file

```goi
FAB { icon: add }
FAB { icon: edit label: "Compose" variant: extended }
FAB { icon: add variant: small }
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
fab := ui.MustByID[*ui.FAB](w, "compose")
fab.OnClick(func(ui.ClickEvent) { openEditor() })
```

Every element also accepts the [common properties](../components.md#common-properties).
