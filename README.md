# goui

A declarative GUI framework for Go, rendered with WebGPU. The window is described
in a small QML-like file; Go finds elements by id and attaches behaviour.

```go
// app.ui
// Window {
//     title: "Counter"
//     Column {
//         padding: 24
//         spacing: 12
//         align: center
//         Text   { id: label text: "0" fontSize: 48 color: #ffffff }
//         Button { id: inc   text: "Increase" background: #3b82f6 radius: 8 }
//     }
// }
```

```go
app, _ := ui.NewApp()
win, _ := app.LoadWindow("app.ui", ui.WithHotReload(true))

count := 0 // outside Bind: survives hot reload
win.Bind(func(w *ui.Window) error {
	label := ui.MustByID[*ui.Text](w, "label")
	ui.MustByID[*ui.Button](w, "inc").OnClick(func(ui.ClickEvent) {
		count++
		label.Text.Set(strconv.Itoa(count))
	})
	return nil
})
app.Run()
```

## Requirements

- Go 1.23 or newer (developed on 1.27).
- A C toolchain and GLFW on the machine (`glfw3` headers).
- A Vulkan, Metal, D3D12 or OpenGL ES driver. No `wgpu-native` binary is needed:
  the renderer is pure Go (`gogpu/wgpu`).

On Linux, build with the internal linker, because GLFW's cgo forces the external
one and goffi's dynamic imports do not survive it:

```sh
go build -ldflags=-linkmode=internal ./...
```

Wayland is the default build; use `-tags x11` for X11.

## Run the examples

```sh
make counter   # counter with hot reload
make hello     # rounded corners, borders, text
```

## What it does

- `.ui` files: literals only, no scripting. Elements, properties, ids, with
  diagnostics that point at `file:line:col`.
- Typed Go API: `Prop[T]` cells, `Event[E]`, `ByID[T]`.
- Layout: Column, Row, Stack, flex grow, padding, margin, min/max width.
- Rendering: instanced SDF rounded rects with borders and anti-aliasing, text
  through HarfBuzz-quality shaping and a grayscale atlas, HiDPI aware.
- Input: clicks, hover, keyboard focus, text editing, Tab order groundwork.
- Hot reload: edit the file, the window updates; broken files keep the last good
  tree and print diagnostics.

See [docs/SYNTAX.md](docs/SYNTAX.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
and [docs/DECISIONS.md](docs/DECISIONS.md).

## Not in v1

Expressions or bindings in `.ui`, animations, theming, accessibility tree,
multiple windows, drag and drop, custom components in `.ui`, and the Checkbox,
Scroll and Image widgets.

## Tests

```sh
make test   # go test -race ./...
make vet
```

Parser, schema, tree, layout and paint are pure Go and run headless; only the
renderer and the window need a display.
