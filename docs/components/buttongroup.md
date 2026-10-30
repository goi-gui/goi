# ButtonGroup

A row of related buttons that share one shape and one gap.

[All components](../components.md)

## In a `.ui` file

```ui
ButtonGroup {
	spacing: 8
	Button { label: "Day" variant: outlined }
	Button { label: "Week" variant: outlined }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `disabled` | bool | dims the element and blocks input |

As a container it also accepts `spacing`/`gap` and `align`.

## From Go

```go
group := ui.MustByID[*ui.ButtonGroup](w, "group")
group.Spacing.Set(12)
```

Every element also accepts the [common properties](../components.md#common-properties).
