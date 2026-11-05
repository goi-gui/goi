# Window

The root of the tree. It owns the title, the theme, the client area and the
native window's own chrome: size limits, resize and fullscreen.

[All components](../components.md)

## In a `.goi` file

```goi
Window {
	id:            main
	title:         "My app"
	theme:         light   // light | dark | a name the app registered
	titleBar:      system  // system | custom
	width:         900
	height:        600
	minWidth:      420     // the window cannot be dragged smaller
	minHeight:     360
	maxWidth:      1200    // ...nor larger
	maxHeight:     900
	disableResize: false   // true: no user resize, and no maximize
	fullscreen:    false   // true: fill the display
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `title` | string | window title |
| `theme` | string | `light` or `dark` |
| `titleBar` | enum | `system` (default) or `custom` |
| `minWidth`, `minHeight` | number | the smallest the user can drag the window |
| `maxWidth`, `maxHeight` | number | the largest; `-1` (default) means no limit |
| `disableResize` | bool | `true` turns off the user's resize and the maximize button |
| `fullscreen` | bool | `true` fills the display |

`minWidth`/`maxWidth` (and their height pair) are common properties, so a child
can use them to bound its own layout; on `Window` they also constrain the native
window. The limits are logical pixels, so they follow the display scale.

`disableResize: true` removes the resize edges and the title bar's maximize
button, and `Window.ToggleMaximize` becomes a no-op. `fullscreen` is a separate
switch and still works, so a kiosk can ask for a fixed size in windowed mode and
fill the screen when the user asks for it.

A declared `maxWidth`/`maxHeight` also disables maximizing: the maximized size
would exceed the cap the design asked for, so a maximize from the title bar is
undone on the next frame and `ToggleMaximize` does nothing. Leave the maximum
out (`-1`) for a window that should be able to fill the screen. Fullscreen is
never capped, which is the point of the separate switch.

The limits are enforced on every size change the window sees — a drag, a
compositor request, a maximize that is being undone — not only at startup, so a
compositor that ignores the window's size hints still cannot make the client
draw outside the design's bounds. A window whose current size falls outside a
newly declared limit is resized to fit at once.

All six are read on every frame, not just at startup, so an expression or a
`Set` from Go reaches the platform without a reload:

```goi
Window { minWidth: bridge.MinW }   // -1 means no limit here too
```

```go
w.Root().DisableResize.Set(true)
w.Root().Fullscreen.Set(true)

// The limits have setters too; -1 clears one.
w.Root().SetMinWidth(600)
w.Root().SetMinHeight(400)
w.Root().SetMaxWidth(-1)
w.Root().SetMaxHeight(-1)
```

A window whose current size falls outside a newly set limit is resized to fit,
which is what a design asking for `minWidth: 600` on a 480-wide window needs.

On a maximized window the limits do not apply while it stays maximized (the
compositor owns that geometry, and resizing it there is a protocol violation);
that is why a declared maximum turns maximize off instead.

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

// Resize, fullscreen and the size limits are properties, so state lives where
// every other property does.
w.Root().DisableResize.Set(true)
w.Root().Fullscreen.Set(true)
w.Root().SetMinWidth(600)

// A custom scheme, registered under a name the file can select.
brand := *ui.LightTheme()
brand.Colors.Primary = 0x00695CFF
win, _ := app.LoadWindowFile("app.goi", ui.WithTheme("brand", &brand))
```

Every element also accepts the [common properties](../components.md#common-properties).
