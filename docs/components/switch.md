# Switch

A single on/off control with an optional label and optional glyph in the thumb.

[All components](../components.md)

## In a `.ui` file

```ui
Switch { label: "Dark mode" checked: true }
Switch { label: "Notifications" iconOn: check iconOff: close }
Switch { label: "Theme" iconOn: dark_mode iconOff: light_mode }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `variant` | enum | visual style; see the page |
| `disabled` | bool | dims the element and blocks input |
| `checked` | bool | on/off state |
| `icon` | string | leading Material Symbols ligature |
| `iconOn` | string | thumb glyph when checked; overrides `icon` |
| `iconOff` | string | thumb glyph when unchecked; overrides `icon` |


## From Go

```go
dark := ui.MustByID[*ui.Switch](w, "theme")
dark.OnToggle(func(on bool) {
	theme := "light"
	if on {
		theme = "dark"
	}
	w.Root().Theme.Set(theme)
})
```

Every element also accepts the [common properties](../components.md#common-properties).
