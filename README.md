# goi

A declarative GUI framework for Go, rendered with WebGPU. The window is described
in a small QML-like file; Go finds elements by id and attaches behaviour.

```go
// app.goi
Window {
    title: "Counter"
    Column {
        padding: 24
        spacing: 12
        align: center
        Text   { id: label text: "0" role: displayLarge }
        Button { id: inc   label: "Increase" icon: add }
    }
}
```

```go
app, _ := ui.NewApp()
win, _ := app.LoadWindowFile("app.goi", ui.WithHotReload(true))

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

A `.goi` file can import fragments and pass them values, so a page does not have
to be one long file. An import names its alias first, as in Go:

```go
// Main.goi
import Sidebar "./Sidebar.goi"

Window {
    Row {
        Sidebar { title: "Mail" width: 300 }
    }
}
```

```go
// Sidebar.goi
props { Title string }
Column {
    Text { text: props.Title role: "titleLarge" }
}
```

Designs load from an **asset container**, an `io/fs.FS` plus a path inside it:

```go
// Development: real files, hot reload watches every file the design reads.
win, _ := app.LoadWindow(ui.DirFS("ui"), "Main.goi", ui.WithHotReload(true))

// Production: one executable, no files on disk.
//go:embed ui
var assets embed.FS
win, _ := app.LoadWindow(assets, "ui/Main.goi")
```

`ui.DirFS` is `os.DirFS` that remembers its directory, which is what hot reload
needs; a plain `os.DirFS` or an `embed.FS` works for loading but cannot be
watched. `ui.LoadWindowFile(path)` is the single-file shortcut.

## Requirements

- Go 1.23 or newer (developed on 1.27).
- A C toolchain and SDL3 on the machine (`sdl3` via pkg-config). SDL picks
  Wayland or X11 at runtime, so one binary runs on both and there is no build tag
  to choose.
- A Vulkan, Metal, D3D12 or OpenGL ES driver. No `wgpu-native` binary is needed:
  the renderer is pure Go (`gogpu/wgpu`).

The renderer reaches the GPU through its own FFI, so the binary still needs the
internal linker even though the only cgo left is the SDL shim:

```sh
go build -ldflags=-linkmode=internal ./...
```

A custom title bar (`Window { titleBar: custom }`) hides the platform
decorations and lets the design draw and drag its own; see
[docs/components/titlebar.md](docs/components/titlebar.md).

## Run the examples

```sh
make counter   # counter with hot reload
make gallery   # every Material 3 component, one category page at a time
make hello     # rounded corners, borders, text
make scroll    # scrolling page, horizontal strip, ripple
make typography # type scale, five weights, wrapping
make composition # one design split across Main.goi, Sidebar.goi and StatCard.goi
make theme     # built-in schemes plus one registered by name
make datalist  # bridge values in a for loop, with a row handler
make format    # formatf, arithmetic and bridge zero values
make bridge    # every event property, and a fragment fed from the bridge
make search    # a button and a text field reading another node by id
go run ./cmd/goicheck -layout examples/*/*.goi  # syntax plus a headless layout check
go run ./cmd/goifmt -l .                        # list unformatted .goi files
```

## What it does

- `.goi` files: literals only, no scripting. Elements, properties, ids, with
  diagnostics that point at `file:line:col`.
- Composable designs: `import Sidebar "./Sidebar.goi"`, a typed `props`
  block the instance fills (`props.Title`, zero value when it passes nothing,
  plus lists and handlers), named slots (`$header`) and `$children`, loaded
  from an `io/fs.FS` so `embed.FS` and `os.DirFS` both work. An instance's `id`
  scopes the fragment's own ids (`first.title`), so one fragment can be used
  many times with behaviour attached to each copy.
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
- `goifmt`: a gofmt for `.goi`. It aligns declaration fields and property
  values in one column, puts one property per line, separates imports and
  declaration blocks by one blank line, indents every body, keeps a comment
  cuddled to what it explains, and collapses extra blank lines. It runs on save
  through `goipls` and as `go run ./cmd/goifmt -w`.
- Go's pure builtins in a value: `len`, `cap`, `min`, `max`, plus `formatf` for
  the `fmt` verbs.
- Go-shaped declarations and control flow: `bridge { Count int }`,
  `type Row { Title string }`, `if x { } else { }` and
  `for i, row := range bridge.Rows { }` read exactly like Go.
  `formatf("%d items", bridge.Count)` formats with Go's verbs, and `+` adds
  numbers but joins anything else. Every primitive type, `[]T`, `map[string]T`
  and `func(...)` work; a variable starts at its type's zero value. A Go struct
  reaches a design through its `goi` tag (`goi:"fullName"`; `goi:"-"` hides a
  field, and so does leaving it unexported).
- Value expressions on every property type: `width: side.width / 2`,
  `width: 50%` of the parent, comparisons and `&& || !`, and a conditional
  (`trailingIcon: app.dark ? "light_mode" : "dark_mode"`). `app.theme`,
  `app.dark`, `window.width`/`height` and any element's property by id
  (`searchField.text`) are readable from a design.
- Colors by M3 role: `background: surfaceContainerHigh` follows the scheme,
  while a hex literal stays a deliberate fixed color.
- Themes: the baseline `light` and `dark` schemes plus any scheme the app
  registers with `ui.WithTheme("brand", t)`, selected in the file with
  `theme: brand`. Start from `ui.LightTheme()`/`ui.DarkTheme()` and change the
  roles you need; `app.dark` follows the resolved scheme's mood, not its name.
- Images: `Image { src: "photo.png" fit: cover }` decodes PNG, JPEG or GIF once
  and packs every bitmap into one GPU atlas.
- Bridge: `bridge { OnOpen func(id string) }` declares the handlers a design
  calls from any event property (`onClick: bridge.OnOpen(row.ID)`), with the
  arguments evaluated when the event fires and `event` carrying the event's own
  payload; `ui.SetHandler` supplies the body. `examples/bridge`
  (`make bridge`) runs every event.
- Lists from Go: a hidden template in the `.goi` file plus `ui.Instantiate` makes
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

## Editor support

The Zed extension lives in [goi-gui/zed-goi](https://github.com/goi-gui/zed-goi):
syntax highlighting, brackets, indentation and an element outline. It starts
`goipls`, the language server in `cmd/goipls`, which adds diagnostics (including
errors inside an imported fragment), completion for elements, properties,
values, icon names, ids, declaration types, `bridge.`/`props.` values, handler
calls, `if`/`for` statements, the `formatf` builtin and a fragment instance's
props (read from the imported file), hover docs for every declaration and
fragment, a document outline and go-to-definition for imports and id
references:

```sh
go install github.com/ksckaan1/goi/cmd/goipls@latest
```

See [goi-gui/zed-goi](https://github.com/goi-gui/zed-goi) for the local install
steps.

See [docs/components.md](docs/components.md) for every element with its
properties and Go examples, and [docs/SYNTAX.md](docs/SYNTAX.md),
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md),
[docs/DECISIONS.md](docs/DECISIONS.md) for the language, the internals and the
reasoning.

## Not in v1

Bindings that push Go state back into `.goi` values (the file is evaluated, not
observed), dynamic color and custom themes, an accessibility tree, multiple
windows, drag and drop, required fragment props (a prop always has a default or
the type's zero value), state and styles declared inside a fragment,
drag-to-fling (the wheel does coast), and caret mapping inside right-to-left
text. IME composition shows as underlined preedit on Wayland.
`go run ./cmd/goicheck -layout` lays a design out headlessly and reports nodes
that cannot draw; a fragment is previewed on its own by filling its props with
their defaults.

## Tests

```sh
make test   # go test -race ./...
make vet
```

Parser, schema, tree, layout and paint are pure Go and run headless; only the
renderer and the window need a display.
