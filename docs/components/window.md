# Window

The root of the tree. It owns the title, the theme and the client area; the platform window supplies the size.

[All components](../components.md)

## In a `.goi` file

```goi
Window {
	id: main
	title: "My app"
	theme: light          // light | dark | a name the app registered
	titleBar: system      // system | custom
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `title` | string | window title |
| `theme` | string | `light` or `dark` |
| `titleBar` | enum | `system` (default) or `custom` |

`titleBar` is read when the window is created, so changing it needs a restart.
`custom` hides the platform title bar; draw your own with
[TitleBar](titlebar.md) and wire its buttons to `Window.Minimize`,
`Window.ToggleMaximize` and `Window.Close`.

A `custom` window is frameless, so the design owns the whole frame: the outer
6 logical pixels resize it, and the title bar moves it. Both are handled by the
platform, so a press on a resize edge or on the drag region never turns into a
click on whatever is underneath.

## From Go

```go
win, err := app.LoadWindow(ui.DirFS("."), "app.goi")
if err != nil {
	return err
}
win.Bind(func(w *ui.Window) error {
	w.Root().Theme.Set("dark")
	return nil
})

// Window controls, usable from any handler in Bind.
w.Minimize()
w.ToggleMaximize()
if w.Maximized() {
	// the window is maximized
}
w.Close()

// A custom scheme, registered under a name the file can select.
brand := *ui.LightTheme()
brand.Colors.Primary = 0x00695CFF
win, _ := app.LoadWindowFile("app.goi", ui.WithTheme("brand", &brand))
```

Every element also accepts the [common properties](../components.md#common-properties).
