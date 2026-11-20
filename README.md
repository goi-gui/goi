<p align="center">
  <img src="./goi-logo-simple.svg" alt="goi logo" width="120">
</p>

<h1 align="center">goi</h1>

<p align="center">
  A declarative GUI framework for Go, rendered with WebGPU on Linux, Windows and macOS.
</p>

<p align="center">
  <a href="https://goi.kaanksc.com"><img src="https://img.shields.io/badge/docs-goi.kaanksc.com-6750A4" alt="Documentation"></a>
  <a href="https://pkg.go.dev/github.com/goi-gui/goi"><img src="https://pkg.go.dev/badge/github.com/goi-gui/goi.svg" alt="Go Reference"></a>
  <a href="https://github.com/goi-gui/goi/actions/workflows/ci.yml"><img src="https://github.com/goi-gui/goi/actions/workflows/ci.yml/badge.svg?branch=master" alt="CI status"></a>
  <a href="https://github.com/goi-gui/goi/releases/latest"><img src="https://img.shields.io/github/v/release/goi-gui/goi" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/goi-gui/goi" alt="MIT license"></a>
</p>

<p align="center">
  <a href="https://goi.kaanksc.com"><b>Documentation</b></a> ·
  <a href="https://github.com/goi-gui/examples">Examples</a> ·
  <a href="https://github.com/goi-gui/goi/releases">Releases</a>
</p>

goi describes a window in `.goi` files: elements, properties, ids, `if`/`else`
and `for … range`. Go supplies the state and the handlers through a typed
bridge. The same design runs on Linux (Wayland and X11), Windows and macOS,
drawn with WebGPU and hot-reloaded while you edit. The component set follows
[Material 3 Expressive](https://m3.material.io/blog/building-with-m3-expressive):
expressive colour, type, shape and motion, with light and dark schemes built
from a seed colour.

## Shopping

One `.goi` file and a small Go entry point: a rail of categories that becomes a
bottom bar on a phone, a picture column that reflows from a row into a column,
size and colour pickers, and a review card. Three colour swatches rebuild both
schemes from their seed, and `themeTransition: wipe` carries each change in.

| Desktop, 1000 × 1000 | Phone, 500 × 1000 |
| --- | --- |
| <img src="https://raw.githubusercontent.com/goi-gui/examples/main/images/shopping-desktop.gif" alt="shopping on a desktop, dark then light scheme" width="430"> | <img src="https://raw.githubusercontent.com/goi-gui/examples/main/images/shopping-mobile.gif" alt="shopping on a phone, dark then light scheme" width="215"> |

Source and build instructions: [examples/shopping](https://github.com/goi-gui/examples/tree/main/shopping).

## Quick start

```sh
go install github.com/goi-gui/goi/cmd/goi@latest
goi setup
mkdir hello && cd hello
goi init --name hello --id com.example.hello --module example.com/hello
goi run
```

`goi setup` installs the CLI, the language server and the formatter. `goi init`
writes the module, `cmd/main.go`, `goi.yaml` and the design under `assets/`.
`goi run` starts it with hot reload on. The [CLI guide](https://goi.kaanksc.com/guide/cli/)
covers the flags, build tags and project layout.

## A design file

```goi
// assets/ui/Main.goi

bridge {
	onSayHello func()
}

Window {
	title: "Hello"
	Column {
		padding: 24
		gap:     12
		Text { text: "Hello, goi" role: headlineMedium }
		Button { label: "Say hello" onClick: bridge.onSayHello() }
	}
}
```

Go supplies `onSayHello` with `ui.SetHandler` and finds the elements it needs by
id. Hot reload updates the open window as the file changes; the
[language guide](https://goi.kaanksc.com/syntax/) covers the rest.

## What you get

- **Declarative designs.** Elements, properties, ids, `if`/`else`,
  `for … range`, imports and fragments, with diagnostics at `file:line:col`.
- **Go state and events.** Typed `bridge` values, `ui.SetData`,
  `ui.SetHandler`, `Prop[T]`, `Event[E]` and `ByID[T]`; state survives hot
  reload.
- **Material 3 Expressive.** The M3 component set, light and dark schemes, seed
  themes, the M3 type scale and Material Symbols.
- **Layout and media.** Flex layout, responsive breakpoints, scrolling and
  snapping, shaped and bidirectional text, PNG, JPEG, GIF and SVG, plus
  Markdown and Mermaid behind their feature tags.
- **Native shell.** Custom window chrome, file dialogs, notifications, keyboard
  and pointer input, and a WebGPU renderer over pure-Go Vulkan, Direct3D 12 and
  Metal backends.
- **Tooling.** `goipls` adds diagnostics, completion, hover docs, an outline and
  formatting on save; `goifmt` formats from the CLI; `goicheck -layout` catches
  collapsed nodes. A [Zed extension](https://github.com/goi-gui/zed-goi) wires
  the language server into the editor.

## Requirements

Go 1.27 or newer and the **SDL3 runtime library**. purego loads it at run time,
so there is no C toolchain, SDL development package or `pkg-config` to install.
The supported build is cgo-free:

```sh
CGO_ENABLED=0 go build -tags "nofakecgo all" ./...
```

Optional features (`emoji`, `svg`, `markdown`, `code`, `mermaid`, `net`,
`dialog`) are opt-in; `all` enables every one. See
[Building](https://goi.kaanksc.com/guide/build/) for the SDL3 package names per
platform, the tag table and the memory knobs.

## Examples

The [examples repository](https://github.com/goi-gui/examples) has a full
component gallery, the shopping page above, a counter, a map, charts, media, a
table and more. The gallery is the visual reference for every component and the
manual test bed for input.

## Contributing

Open an issue or a pull request. Keep changes focused, match the surrounding
style, and run `make test` and `make vet`.

## License

[MIT](LICENSE) © ksckaan1
