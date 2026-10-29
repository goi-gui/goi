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
win, _ := app.LoadWindowFile("app.ui", ui.WithHotReload(true))

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

## One design, several files

A `.ui` file can import fragments and pass them values, so a page does not have
to be one long file:

```go
// Main.ui
// import "./Sidebar.ui" as Sidebar
//
// Window {
//     Row {
//         Sidebar { title: "Mail" width: 300 }
//     }
// }
```

```go
// Sidebar.ui
// param title: ""
// Column {
//     Text { text: $title role: "titleLarge" }
// }
```

Designs load from an **asset container**, an `io/fs.FS` plus a path inside it:

```go
// Development: real files, hot reload watches every file the design reads.
win, _ := app.LoadWindow(ui.DirFS("ui"), "Main.ui", ui.WithHotReload(true))

// Production: one executable, no files on disk.
//go:embed ui
var assets embed.FS
win, _ := app.LoadWindow(assets, "ui/Main.ui")
```

`ui.DirFS` is `os.DirFS` that remembers its directory, which is what hot reload
needs; a plain `os.DirFS` or an `embed.FS` works for loading but cannot be
watched. `ui.LoadWindowFile(path)` is the single-file shortcut.

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
make gallery   # every Material 3 component, one category page at a time
make hello     # rounded corners, borders, text
make scroll    # scrolling page, horizontal strip, ripple
make typography # type scale, five weights, wrapping
make composition # one design split across Main.ui, Sidebar.ui and StatCard.ui
```

## What it does

- `.ui` files: literals only, no scripting. Elements, properties, ids, with
  diagnostics that point at `file:line:col`.
- Composable designs: `import "./Sidebar.ui" as Sidebar` plus `param`/`$param`,
  loaded from an `io/fs.FS` so `embed.FS` and `os.DirFS` both work.
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
- Text: the M3 type scale (`role`), explicit `fontSize` and `weight` (300/400/
  500/700/900 Roboto; other numbers snap to the nearest), and `wrap: true` for
  real word wrapping that reserves the height of every line.
- Value expressions on every property type: `width: side.width / 2`,
  `width: 50%` of the parent, comparisons and `&& || !`, and a conditional
  (`trailingIcon: app.dark ? "light_mode" : "dark_mode"`). `app.theme`,
  `app.dark` and `window.width`/`height` are readable from a design.
- Colors by M3 role: `background: surfaceContainerHigh` follows the scheme,
  while a hex literal stays a deliberate fixed color.
- Images: `Image { src: "photo.png" fit: cover }` decodes PNG, JPEG or GIF once
  and packs every bitmap into one GPU atlas.
- Lists from Go: a hidden template in the `.ui` file plus `ui.Instantiate` makes
  N rows without hand-building nodes; `ui.Remove` takes one away.
- Motion: `app.Tween` and `app.Animate` ease a function or a `Prop` over time,
  and the loop sleeps again when the last one finishes.
- Scrolling containers draw the M3 thumb, so a long page shows where it is.
- Visibility on every element, components included: `visible: false` (also
  responsive) or `SetVisible` from Go removes it from layout, paint and input.
- Rendering: instanced SDF rounded rects, circles, arcs and shadows with borders
  and anti-aliasing, text through HarfBuzz-quality shaping and a grayscale
  atlas, HiDPI aware.
- Input: clicks with Material ripples, hover, text editing with selection and
  clipboard shortcuts, wheel and drag scrolling. Tab and Shift+Tab walk the
  visible controls, Enter and Space press the focused one, and focus draws the
  M3 outline. Any node can take a click (`OnClick`), not only a component, so a
  scrim or a custom hit area needs no extra element type.
- Overlays in order: shapes and glyphs share one instance buffer, so a drawer, a
  dialog or a snackbar paints over the page exactly as the tree orders it.
- Window-level keys: `win.OnKey` sees every key press, focused or not, which is
  how a modal drawer closes on Escape.
- Text editing: selection, clipboard, word and line keys, a value that scrolls
  to keep the caret in view, and IME composition drawn as underlined preedit.
  Text is shaped in bidirectional runs, so Arabic and Hebrew join and read
  right to left.
- Hot reload: edit the file, the window updates; broken files keep the last good
  tree and print diagnostics.

See [docs/SYNTAX.md](docs/SYNTAX.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
and [docs/DECISIONS.md](docs/DECISIONS.md).

## Not in v1

Bindings that push Go state back into `.ui` values (the file is evaluated, not
observed), dynamic color and custom themes, an accessibility tree, multiple
windows, drag and drop, custom components in `.ui`, drag-to-fling (the wheel does
coast), and caret mapping inside right-to-left text. IME composition shows as underlined preedit on
Wayland. `go run ./cmd/uicheck -layout` lays a design out headlessly and reports
nodes that cannot draw.

## Tests

```sh
make test   # go test -race ./...
make vet
```

Parser, schema, tree, layout and paint are pure Go and run headless; only the
renderer and the window need a display.
