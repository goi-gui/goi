# Snackbar

A short, transient message at the bottom of the screen.

[All components](../components.md)

## In a `.goi` file

```goi
Snackbar { label: "Saved to your library" width: 320 }
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | the main text |
| `variant` | enum | visual style; see the page |

## From Go

```go
snack := ui.MustByID[*ui.Snackbar](w, "snack")
snack.Label.Set("Saved")
snack.SetVisible(true)
time.AfterFunc(3*time.Second, func() {
	app.Post(func() { snack.SetVisible(false) })
})
```

Every element also accepts the [common properties](../components.md#common-properties).
