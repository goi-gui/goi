# Window

The root of the tree. It owns the title, the theme and the client area; the platform window supplies the size.

[All components](../components.md)

## In a `.ui` file

```ui
Window {
	id: main
	title: "My app"
	theme: light          // light | dark
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `title` | string | window title |
| `theme` | string | `light` or `dark` |


## From Go

```go
win, err := app.LoadWindow(ui.DirFS("."), "app.ui")
if err != nil {
	return err
}
win.Bind(func(w *ui.Window) error {
	w.Root().Theme.Set("dark")
	return nil
})
```

Every element also accepts the [common properties](../components.md#common-properties).
