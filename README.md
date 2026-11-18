<p align="center">
  <img src="./goi-logo-simple.svg" alt="goi logo" width="140">
</p>

<h1 align="center">goi</h1>

<p align="center">A declarative GUI framework for Go, rendered with WebGPU on Linux, Windows and macOS.</p>

goi describes a window in `.goi` files: elements, properties, ids, `if`/`else`
and `for` loops. Go finds elements by id, holds the state and attaches the
behaviour. Hot reload is built in: edit a `.goi` file and the open window
updates without a restart. The same project builds for Linux (Wayland and X11),
Windows and macOS, drawing through Vulkan, Direct3D 12 or Metal.

The component set and design language follow
[Material 3 Expressive](https://m3.material.io/blog/building-with-m3-expressive):
expressive colour, type, shape and motion, with light and dark schemes.

## Shopping

One `.goi` file and a small `main.go`: a rail of categories that becomes a
bottom bar on a phone, a picture column that turns from a row into a column,
size and colour pickers, and a review card. Both schemes are built from one rose
seed with `ui.NewThemeFromSeed`.

| | Light | Dark |
| --- | --- | --- |
| **Desktop**<br>1000 × 800 | <img src="https://raw.githubusercontent.com/goi-gui/examples/main/images/shopping-light.png" alt="shopping on a desktop, light scheme" width="430"> | <img src="https://raw.githubusercontent.com/goi-gui/examples/main/images/shopping-dark.png" alt="shopping on a desktop, dark scheme" width="430"> |
| **Phone**<br>500 × 1000 | <img src="https://raw.githubusercontent.com/goi-gui/examples/main/images/shopping-mobile-light.png" alt="shopping on a phone, light scheme" width="215"> | <img src="https://raw.githubusercontent.com/goi-gui/examples/main/images/shopping-mobile-dark.png" alt="shopping on a phone, dark scheme" width="215"> |

Run it from the [examples](https://github.com/goi-gui/examples/tree/main/shopping)
checkout with `make shopping`.

## Counter

The design declares the values it reads and the handlers it calls in a `bridge`
block. Go supplies both.

```goi
// assets/ui/Main.goi

bridge {
	Count   int
	onInc   func()
	onDec   func()
	onReset func()
}

Window {
	title: "Counter"

	Column {
		crossAxisAlign: center
		mainAxisAlign:  center
		gap:            20

		Text {
			text: "Counter"
			role: titleLarge
		}

		Row {
			gap:            20
			crossAxisAlign: center
			mainAxisAlign:  center

			Button {
				label:    "-"
				onClick:  bridge.onDec()
				disabled: bridge.Count == 0
			}

			Text {
				text: formatf("%02d", bridge.Count)
				role: displayLarge
			}

			Button {
				label:   "+"
				onClick: bridge.onInc()
			}
		}

		IconButton {
			icon:     refresh
			onClick:  bridge.onReset()
			disabled: bridge.Count == 0
		}
	}
}
```

```go
app, _ := ui.NewApp()
win, _ := app.LoadWindow("ui/Main.goi")

// count lives outside Bind: Bind runs again after every hot reload
count := 0

win.Bind(func(w *ui.Window) error {
	ui.SetData(w, "Count", count)
	ui.SetHandler(w, "onInc", func() {
		count++
		ui.SetData(w, "Count", count)
	})
	ui.SetHandler(w, "onDec", func() {
		count--
		ui.SetData(w, "Count", count)
	})
	ui.SetHandler(w, "onReset", func() {
		count = 0
		ui.SetData(w, "Count", count)
	})
	return nil
})

app.Run()
```

`SetData` feeds `bridge.Count`; `SetHandler` supplies the body a bridge call
runs. The label follows the value without a manual repaint, and `count`
keeps its value when `Bind` runs again after a hot reload.

## Conditionals and loops

`if`/`else` and `for ... range` read like Go, and a list comes from the bridge:

```goi
// inbox.goi

bridge {
	Loading  bool
	Messages []Message
}

type Message {
	From    string
	Subject string
	Unread  bool
}

Window {
	title: "Inbox"

	Column {
		padding: 24
		spacing: 8

		if bridge.Loading {
			Text {
				text: "Loading…"
			}
		} else {
			for _, message := range bridge.Messages {
				ListItem {
					label:      message.From
					supporting: message.Subject
					selected:   message.Unread
				}
			}
		}
	}
}
```

```go
ui.SetData(w, "Loading", false)
ui.SetData(w, "Messages", messages) // any slice of structs
```

The rows rebuild when the list is set; a scalar such as `bridge.Loading` only
toggles what the `if` shows.

## Several files

A design can import fragments and pass them values, so a page does not have to be
one long file.

```goi
// assets/ui/Main.goi

import Sidebar "./Sidebar.goi"

Window {
	id:    main
	title: "Mail"

	Row {
		Sidebar {
			title: "Inbox"
		}

		Column {
			flex:    1
			padding: 24

			Text {
				text: "Select a message"
				role: titleLarge
			}
		}
	}
}
```

```goi
// assets/ui/Sidebar.goi

props {
	title string
}

Column {
	width:      280
	padding:    12
	spacing:    6
	background: theme.surfaceContainer

	Text {
		text: props.title
		role: titleLarge
	}

	NavigationItem {
		label:       "Inbox"
		leadingIcon: inbox
		selected:    true
	}

	NavigationItem {
		label:       "Starred"
		leadingIcon: star
	}

	NavigationItem {
		label:       "Sent"
		leadingIcon: send
	}
}
```

`LoadWindow` reads the entry from the `assets` directory in `goi.yaml`; the
designs live in its `ui/` subdirectory. Hot reload watches every file the design
reads:

```go
win, _ := app.LoadWindow("ui/Main.goi")
```

`goi build` embeds the assets directory automatically, so the same call works in
a shipped binary.

## Features

- **Declarative design files.** Elements, properties, ids, `if`/`else` and
  `for ... range`; diagnostics point at `file:line:col`.
- **State and events in Go.** `bridge` declarations, `ui.SetData`,
  `ui.SetHandler`, typed `Prop[T]`, `Event[E]` and `ByID[T]`.
- **Composition.** Imports, typed `props`, named slots and `$children`, scoped
  ids, loading an embedded design.
- **Material 3 Expressive.** Buttons, text fields, cards, dialogs, sheets, menus, navigation,
  progress and more, with light and dark schemes, Roboto and Material Symbols.
- **Layout.** Column, Row and Stack, Wrap, Grid, flex sizing, responsive
  breakpoints, padding and margin, and scrolling containers.
- **Text and media.** M3 type scale, word wrapping, bidirectional shaping, PNG,
  JPEG, GIF and SVG with theme colours.
- **Native shell.** Window chrome control, file dialogs, notifications, keyboard
  input and hot reload.
- **Language server.** `goipls` adds diagnostics (including inside imported
  fragments), completion, hover docs, document outline and go-to-definition,
  plus formatting on save.
- **Editor extension.** A [Zed extension](https://github.com/goi-gui/zed-goi)
  brings syntax highlighting, brackets, indentation, an element outline and the
  `.goi` file icon. It is the only editor extension so far; elsewhere the
  language server still works over stdio.
- **Formatter.** `goifmt` aligns fields and properties, one property per line,
  and runs through the language server or as `go run ./cmd/goifmt -w`.
- **Renderer.** Instanced WebGPU drawing below a small SDL3 platform layer,
  HiDPI aware: one binary draws through Vulkan on Wayland and X11, DX12 on
  Windows and Metal on macOS.

See the [documentation](https://goi-gui.github.io/docs/) for the language and
every element.

## Start a project

```sh
go install github.com/goi-gui/goi/cmd/goi@latest
goi setup
goi init --name hello --id com.example.hello --module example.com/hello
goi run
```

`goi init` asks for any value a flag did not give, then creates the module, a
`cmd/main.go`, a `goi.yaml` and an `assets/ui/Main.goi` with a centred hello
world; the command loads it through `app.LoadWindow("ui/Main.goi")` and hot
reload is on by
default. `goi run` reads `runFlags` and `goi build` reads `buildFlags` from
`goi.yaml`, where both default to `all`; `nofakecgo` and `CGO_ENABLED=0` are
always applied. `goi build` writes
`dist/<os>-<arch>/<name>` with hot reload off. `goi setup` and `goi update` are
the same command: they install goi, goipls and goifmt, and bump the project's
goi dependency when the directory has a `go.mod`. `goi doctor` checks Go, SDL3,
the GPU backend and the companion tools. See
[the CLI guide](https://goi-gui.github.io/guide/cli) for the layout and the
feature list.

## Building

goi builds on Linux, Windows and macOS without a C toolchain. Everything,
including the SDL3 window layer, is pure Go: `internal/platform` loads the
**SDL3 runtime library** through purego, so there are no headers,
`pkg-config` files or C compiler to install. There is no build tag to pick a
platform: the same binary asks SDL which driver it got and hands the matching
native handle to the renderer.

The supported build is cgo-free. `CGO_ENABLED=0` keeps cgo out, and
`-tags nofakecgo` lets goffi and purego share one fake C runtime; both are
required, and there is no cgo build.

```sh
# Linux — only the SDL3 runtime library (libsdl3-0 on recent Debian/Ubuntu,
# sdl3 on Arch, or SDL3 built from source and installed)
CGO_ENABLED=0 go build -tags "nofakecgo all" ./...

# macOS — brew installs the runtime library
brew install sdl3
CGO_ENABLED=0 go build -tags "nofakecgo all" ./...

# Windows — put SDL3.dll next to the executable (the SDL3 release or MSYS2's
# mingw-w64-x86_64-sdl3); no MSYS2 shell is needed for the build itself
CGO_ENABLED=0 go build -tags "nofakecgo all" ./...
```

Add `-ldflags="-H windowsgui"` on Windows for a program without a console
window.

Optional features are opt-in: the core build has no colour emoji, SVG,
Markdown, code highlighting, network, desktop dialogs or Mermaid. Name the ones
the app uses, or take them all with `all`:

```sh
# everything
CGO_ENABLED=0 go build -tags "nofakecgo all" -ldflags="-s -w" -o myapp ./cmd/myapp

# only the features this app needs
CGO_ENABLED=0 go build -tags "nofakecgo emoji svg net dialog" \
	-ldflags="-s -w" -o myapp ./cmd/myapp
```

See [Building](https://goi-gui.github.io/guide/build) for the tag table, the measured
sizes and the memory knobs (`App.MSAA`, `App.ImageCache`, `App.VideoBuffer`).

The renderer picks its backend from the surface it is given: Vulkan through
Wayland or X11, Direct3D 12 on Windows, Metal on macOS. All four go through
`github.com/gogpu/wgpu`'s pure-Go backends, so no extra GPU SDK is needed.

## Contributing

Contributions are welcome: open an issue or a pull request. Keep changes focused
and match the surrounding style, then run:

```sh
make test
make vet
```

## License

[MIT](LICENSE) © ksckaan1
