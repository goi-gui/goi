# Switch

A single on/off control with an optional label and optional glyph in the thumb.

[All components](../components.md)

## In a `.goi` file

```goi
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

A switch with a glyph uses the larger thumb in both states, with the same 4dp
inset on every side. The glyph is `Primary` on the checked thumb and the surface
colour (white in the light scheme) on the unchecked one.

Toggling animates: the thumb slides from one end to the other while it grows
from 16dp to 24dp, the track fills in, the unchecked outline fades out and the
glyph cross-fades at the half way point. The motion follows a target rather than
a fixed start, so flipping the switch again mid-travel turns it around instead
of restarting, and it always settles exactly on the end state so an idle window
stops drawing. `checked: true` in a design starts settled; nothing slides on
load or on a hot reload. The handle is UI state, not a property: it lives in
`Component.TogglePos` and is advanced by `ui`'s frame tick.

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
