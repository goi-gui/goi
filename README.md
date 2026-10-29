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
//         Text   { id: label text: "0" role: displayLarge }
//         Button { id: inc   label: "Increase" icon: add }
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
make gallery   # every Material 3 component on one page
make hello     # rounded corners, borders, text
make scroll    # scrolling page, horizontal strip, ripple
```

## What it does

- `.ui` files: literals only, no scripting. Elements, properties, ids, with
  diagnostics that point at `file:line:col`.
- Typed Go API: `Prop[T]` cells, `Event[E]`, `ByID[T]`.
- Material 3 components as core elements: buttons, FABs, chips, selection
  controls, sliders, text fields, cards, dialogs, sheets, lists, menus,
  app bars, navigation, tabs, progress, pickers, carousel and toolbar. They ship
  with the light and dark baseline schemes, Roboto and Material Symbols.
- Layout: Column, Row, Stack with `Positioned` anchoring, Wrap and Grid, flex
  grow and shrink, padding, margin, min/max width, and `scroll: true` on any
  container (Row sideways, the rest down).
- Responsive values per property: `padding: { base: 8, md: 16 }`, with
  Tailwind's `sm`/`md`/`lg`/`xl` min-width breakpoints applied on resize.
- Value expressions: `width: side.width / 2` follows another element's
  property, and `width: 50%` is half of the parent's content box.
- Visibility on every element, components included: `visible: false` (also
  responsive) or `SetVisible` from Go removes it from layout, paint and input.
- Rendering: instanced SDF rounded rects, circles, arcs and shadows with borders
  and anti-aliasing, text through HarfBuzz-quality shaping and a grayscale
  atlas, HiDPI aware.
- Input: clicks with Material ripples, hover, keyboard focus, text editing with
  selection and clipboard shortcuts, wheel and drag scrolling, Tab order
  groundwork. Any node can take a click (`OnClick`), not only a component, so a
  scrim or a custom hit area needs no extra element type.
- Overlays in order: shapes and glyphs share one instance buffer, so a drawer, a
  dialog or a snackbar paints over the page exactly as the tree orders it.
- Window-level keys: `win.OnKey` sees every key press, focused or not, which is
  how a modal drawer closes on Escape.
- Text editing: selection, clipboard, word and line keys, a value that scrolls
  to keep the caret in view, and IME composition drawn as underlined preedit.
- Hot reload: edit the file, the window updates; broken files keep the last good
  tree and print diagnostics.

See [docs/SYNTAX.md](docs/SYNTAX.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
and [docs/DECISIONS.md](docs/DECISIONS.md).

## Not in v1

Bindings that push Go state back into `.ui` values (the file is evaluated, not
observed), animations, dynamic color and custom themes, accessibility tree,
multiple windows, drag and drop, custom components in `.ui`, and the Image
widget. IME composition shows as underlined preedit on Wayland.

## Tests

```sh
make test   # go test -race ./...
make vet
```

Parser, schema, tree, layout and paint are pure Go and run headless; only the
renderer and the window need a display.
