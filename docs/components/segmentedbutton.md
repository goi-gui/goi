# SegmentedButton

A horizontal set of segments. Contiguous siblings form one group.

[All components](../components.md)

## In a `.goi` file

```goi
SegmentedButton { label: "Day" width: 84 variant: single }
SegmentedButton { label: "Week" width: 84 variant: single selected: true }
SegmentedButton { label: "Month" width: 96 variant: single }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `icon` | string | leading Material Symbols ligature |
| `variant` | enum | visual style; see the page |
| `size` | enum | M3 size token |
| `disabled` | bool | dims the element and blocks input |
| `selected` | bool | active/selection state |


## From Go

```go
seg := ui.MustByID[*ui.SegmentedButton](w, "week")
seg.OnClick(func(ui.ClickEvent) {
	seg.Selected.Set(true)
})
```

Every element also accepts the [common properties](../components.md#common-properties).
