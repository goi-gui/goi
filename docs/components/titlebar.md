# TitleBar

A `Row` that also moves the window. It draws nothing by itself: the custom
title bar's contents are its children. Use it together with
`titleBar: custom` on the Window.

[All components](../components.md)

## In a `.goi` file

```goi
Window {
	titleBar: custom

	TitleBar {
		height: 44
		padding: 8
		spacing: 8
		align: center

		Text { text: "My app" role: titleMedium }
		Rectangle { flex: 1 }
		IconButton { id: minimize icon: minimize }
		IconButton { id: maximize icon: maximize }
		IconButton { id: close icon: close }
	}
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `spacing` | number | gap between children |
| `gap` | number | alias of `spacing` |
| `align` | enum | cross-axis placement |

## From Go

```go
win.Bind(func(w *ui.Window) error {
	ui.MustByID[*ui.IconButton](w, "minimize").OnClick(func(ui.ClickEvent) {
		w.Minimize()
	})
	ui.MustByID[*ui.IconButton](w, "maximize").OnClick(func(ui.ClickEvent) {
		w.ToggleMaximize()
	})
	ui.MustByID[*ui.IconButton](w, "close").OnClick(func(ui.ClickEvent) {
		w.Close()
	})
	return nil
})
```

A press starts a drag unless it lands on a clickable child (`Button`,
`IconButton`, a text field, …), so window buttons keep their normal click. The
window is moved by the platform, not by the client: on Wayland the compositor
performs `xdg_toplevel.move`, on X11, macOS and Windows the same gesture is
handled natively. Because `titleBar: custom` also drops the platform borders,
the outer 6dp of the window resize it: the same callback that reports the drag
region reports a resize edge there, so a frameless window still has working
borders.

`TitleBar` is otherwise just a `Row`; `visible: app.os == "linux"` picks a
platform-specific design.

Every element also accepts the [common properties](../components.md#common-properties).
