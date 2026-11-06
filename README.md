<p align="center">
  <img src="./goi-logo-simple.svg" alt="goi logo" width="140">
</p>

<h1 align="center">goi</h1>

<p align="center">A declarative GUI framework for Go, rendered with WebGPU.</p>

goi describes a window in `.goi` files: elements, properties and ids, with no
loops or scripting. Go finds elements by id, holds the state and attaches the
behaviour.

## Counter

The design declares the values it reads and the handlers it calls in a `bridge`
block. Go supplies both.

```goi
// counter.goi

bridge {
	Count int
	onInc func()
	onDec func()
}

Window {
	title: "Counter"

	Column {
		padding:        24
		spacing:        16
		crossAxisAlign: center

		Text {
			text: formatf("%d", bridge.Count)
			role: displayLarge
		}

		Row {
			spacing: 12

			Button {
				id:      dec
				label:   "−"
				variant: outlined
				onClick: bridge.onDec()
			}

			Button {
				id:      inc
				label:   "+"
				onClick: bridge.onInc()
			}
		}
	}
}
```

```go
var count int // outside Bind: Bind runs again after every hot reload

func main() {
	app, _ := ui.NewApp()
	win, _ := app.LoadWindowFile("counter.goi", ui.WithHotReload(true))

	win.Bind(func(w *ui.Window) error {
		ui.SetData(w, "Count", count)
		if err := ui.SetHandler(w, "onInc", func() {
			count++
			ui.SetData(w, "Count", count)
		}); err != nil {
			return err
		}
		return ui.SetHandler(w, "onDec", func() {
			count--
			ui.SetData(w, "Count", count)
		})
	})

	app.Run()
}
```

`SetData` feeds `bridge.Count`; `SetHandler` supplies the body a bridge call runs.
The label follows the value without a manual repaint.

## Several files

A design can import fragments and pass them values, so a page does not have to be
one long file.

```goi
// ui/Main.goi

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
// ui/Sidebar.goi

props {
	title string
}

Column {
	width:      280
	padding:    12
	spacing:    6
	background: surfaceContainer

	Text {
		text: props.title
		role: titleLarge
	}

	NavigationItem {
		label:    "Inbox"
		icon:     inbox
		selected: true
	}

	NavigationItem {
		label: "Starred"
		icon:  star
	}

	NavigationItem {
		label: "Sent"
		icon:  send
	}
}
```

The directory is the asset container, and hot reload watches every file the
design reads:

```go
win, _ := app.LoadWindow(ui.DirFS("ui"), "Main.goi", ui.WithHotReload(true))
```

For a single binary, pass an embedded filesystem instead:
`app.LoadWindow(assets, "ui/Main.goi")`.

## Features

- **Declarative design files.** Elements, properties and ids; diagnostics point
  at `file:line:col`.
- **State and events in Go.** `bridge` declarations, `ui.SetData`,
  `ui.SetHandler`, typed `Prop[T]`, `Event[E]` and `ByID[T]`.
- **Composition.** Imports, typed `props`, named slots and `$children`, scoped
  ids, loading from any `io/fs.FS`.
- **Material 3.** Buttons, text fields, cards, dialogs, sheets, menus, navigation,
  progress and more, with light and dark schemes, Roboto and Material Symbols.
- **Layout.** Column, Row and Stack, Wrap, Grid, flex sizing, responsive
  breakpoints, padding and margin, and scrolling containers.
- **Text and media.** M3 type scale, word wrapping, bidirectional shaping, PNG,
  JPEG, GIF and SVG with theme colours.
- **Native shell.** Window chrome control, file dialogs, notifications, keyboard
  input and hot reload.
- **Tooling.** `goipls` for diagnostics, completion, hover and go-to-definition
  in the [Zed extension](https://github.com/goi-gui/zed-goi), plus `goifmt`.
- **Renderer.** Instanced WebGPU drawing below a small SDL3 platform layer,
  HiDPI aware, Wayland and X11 from one binary.

See [docs/SYNTAX.md](docs/SYNTAX.md) for the language and
[docs/components.md](docs/components.md) for every element.

## Contributing

Contributions are welcome: open an issue or a pull request. Keep changes focused
and match the surrounding style, then run:

```sh
make test
make vet
```

## License

[MIT](LICENSE) © ksckaan1
