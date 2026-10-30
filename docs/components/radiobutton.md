# RadioButton

One option in a set where exactly one is chosen.

[All components](../components.md)

## In a `.goi` file

```goi
RadioButton { label: "Small" checked: true }
RadioButton { label: "Medium" }
RadioButton { label: "Large" }
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
radios := []*ui.RadioButton{
	ui.MustByID[*ui.RadioButton](w, "small"),
	ui.MustByID[*ui.RadioButton](w, "medium"),
	ui.MustByID[*ui.RadioButton](w, "large"),
}
pick := func(i int) {
	for j, r := range radios {
		r.Checked.Set(j == i)
	}
}
for i, r := range radios {
	i := i
	r.OnToggle(func(bool) { pick(i) })
}
```

Every element also accepts the [common properties](../components.md#common-properties).
