# Tabs

A tab bar that groups `Tab` children.

[All components](../components.md)

## In a `.goi` file

```goi
Tabs {
	Tab { label: "Overview" selected: true }
	Tab { label: "Specs" }
	Tab { label: "Details" }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
tabs := ui.MustByID[*ui.Tabs](w, "tabs")
tabs.OnSelect(func(i int) { showPanel(i) })
```

Every element also accepts the [common properties](../components.md#common-properties).
